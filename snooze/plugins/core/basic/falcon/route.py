#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

import traceback
import sys
from datetime import datetime
from urllib.parse import unquote
from logging import getLogger
from typing import Dict, Any, Union, List, NamedTuple

from dataclasses import dataclass
from typing_extensions import Literal

import falcon
import bson.json_util

from snooze.api.falcon import authorize, FalconRoute
from snooze.utils.parser import parser

log = getLogger('snooze.api')

class ValidationError(RuntimeError):
    '''Raised when the validation fails'''

def convert_type(mytype: type, value: str) -> Union[str, bool, int, None]:
    '''Convert a query string value to a given type. Returns None in case of empty string'''
    if value == '':
        return None
    if mytype == str:
        return str(value)
    if mytype == int:
        return int(value)
    if mytype == bool:
        return (value == 'true')
    else:
        raise Exception(f"Unsupported type {mytype}")

class ParamSchema(NamedTuple):
    '''A named tuple for representing the result key and type of a param'''
    result_name: str
    type: type

SCHEMA = {
    'perpage': ParamSchema('nb_per_page', int),
    'pagenb': ParamSchema('page_number', int),
    'orderby': ParamSchema('orderby', str),
    'asc': ParamSchema('asc', bool),
}

class Route(FalconRoute):
    @authorize
    def on_get(self, req, resp, search='[]', **kwargs):
        ql = None
        if 'ql' in req.params:
            try:
                ql = parser(req.params.get('ql'))
            except Exception:
                ql = None
        if 's' in req.params:
            s = req.params.get('s') or search
        else:
            s = search

        pagination = {}
        for key, value in {**req.params, **kwargs}.items():
            if key in SCHEMA:
                result_key, mytype = SCHEMA[key]
                pagination[result_key] = convert_type(mytype, value)
        try:
            cond_or_uid = bson.json_util.loads(unquote(s))
        except Exception:
            cond_or_uid = s
        if self.inject_payload:
            cond_or_uid = self.inject_payload_search(req, cond_or_uid)
        if ql:
            if cond_or_uid:
                cond_or_uid = ['AND', ql, cond_or_uid]
            else:
                cond_or_uid = ql
        log.debug("Trying search %s", cond_or_uid)
        result_dict = self.search(self.plugin.name, cond_or_uid, **pagination)
        resp.content_type = falcon.MEDIA_JSON
        if result_dict:
            resp.media = result_dict
            resp.status = falcon.HTTP_200
        else:
            resp.media = {}
            resp.status = falcon.HTTP_404

    @authorize
    def on_post(self, req, resp):
        if self.inject_payload:
            self.inject_payload_media(req, resp)
        resp.content_type = falcon.MEDIA_JSON
        log.debug("Trying to insert %s", req.media)
        media = req.media.copy()
        if not isinstance(media, list):
            media = [media]
        rejected = []
        validated = []
        for req_media in media:
            queries = req_media.get('qls', [])
            req_media['snooze_user'] = {
                'name': req.context['user']['user']['name'],
                'method': req.context['user']['user']['method'],
            }

            # Validation
            try:
                self._validate(req_media, req, resp)
            except ValidationError:
                rejected.append(req_media)
                continue

            for query in queries:
                try:
                    parsed_query = parser(query['ql'])
                    log.debug("Parsed query: %s -> %s", query['ql'], parsed_query)
                    req_media[query['field']] = parsed_query
                except Exception as err:
                    log.exception(err)
                    rejected.append(req_media)
                    continue
            validated.append(req_media)
        try:
            result = self.insert(self.plugin.name, validated)
            result['data']['rejected'] += rejected
            resp.media = result
            self.plugin.reload_data(True)
            resp.status = falcon.HTTP_201
            self._audit(result, req)
        except Exception as err:
            log.exception(err)
            resp.media = []
            resp.status = falcon.HTTP_503

    @authorize
    def on_put(self, req, resp):
        if self.inject_payload:
            self.inject_payload_media(req, resp)
        resp.content_type = falcon.MEDIA_JSON
        log.debug("Trying to update %s", req.media)
        media = req.media.copy()
        if not isinstance(media, list):
            media = [media]
        rejected = []
        validated = []
        for req_media in media:
            try:
                self._validate(req_media, req, resp)
            except ValidationError:
                rejected.append(req_media)
                continue
            validated.append(req_media)
        try:
            result = self.update(self.plugin.name, validated)
            result['data']['rejected'] += rejected
            resp.media = result
            self.plugin.reload_data(True)
            resp.status = falcon.HTTP_201
            self._audit(result, req)
        except Exception as err:
            log.exception(err)
            resp.media = []
            resp.status = falcon.HTTP_503

    @authorize
    def on_delete(self, req, resp, search='[]'):
        if 'uid' in req.params:
            cond_or_uid = ['=', 'uid', req.params['uid']]
        else:
            string = req.params.get('s') or search
            try:
                cond_or_uid = bson.json_util.loads(string)
            except Exception:
                cond_or_uid = string
        if self.inject_payload:
            cond_or_uid = self.inject_payload_search(req, cond_or_uid)
        deleted_objects = [{'_old': data} for data in self.search(self.plugin.name, cond_or_uid)['data']]
        log.debug("Trying delete %s" % cond_or_uid)
        result_dict = self.delete(self.plugin.name, cond_or_uid)
        resp.content_type = falcon.MEDIA_JSON
        self._audit({'data': {'deleted': deleted_objects}}, req)
        if result_dict:
            result = result_dict
            resp.media = result
            self.plugin.reload_data(True)
            resp.status = falcon.HTTP_OK
        else:
            resp.media = {}
            resp.status = falcon.HTTP_NOT_FOUND

    def _validate(self, obj, req, resp):
        '''Validate an object and handle the response in case of exception'''
        try:
            self.plugin.validate(obj)
        except Exception as err:
            rejected = obj
            rejected['error'] = f"Error during validation: {err}"
            rejected['traceback'] = traceback.format_exception(*sys.exc_info())
            results = {'data': {'rejected': [rejected]}}
            log.exception(err)
            raise ValidationError("Invalid object")

    def _audit(self, results, req):
        '''Audit the changed objects in a dedicated collection'''
        if self.plugin.metadata.get('audit'):
            messages = []
            for action, objs in results.get('data', {}).items():
                for obj in objs:
                    try:
                        error = obj.pop('error', None)
                        _traceback = obj.pop('traceback', None)
                        old = sanitize(obj.pop('_old', {}))
                        new = sanitize(dict(obj))
                        source_ip = req.access_route[0] if len(req.access_route) > 0 else 'unknown'
                        if action == 'deleted':
                            object_id = old.get('uid')
                        else:
                            object_id = obj.get('uid')
                        try:
                            username = req.context['user']['user']['name']
                            method = req.context['user']['user']['method']
                        except KeyError:
                            username = 'unknown'
                            method = 'unknown'
                        message = {
                            'collection': self.plugin.name,
                            'object_id': object_id,
                            'timestamp': datetime.now().astimezone().isoformat(),
                            'action': action,
                            'username': username,
                            'method': method,
                            'snapshot': new,
                            'source_ip': source_ip,
                            'user_agent': req.user_agent,
                            'summary': diff_summary(old, new),
                        }
                        messages.append(message)
                    except Exception as err:
                        log.exception(err)
                        continue
                    if error:
                        message['error'] = error
                    if _traceback:
                        message['traceback'] = _traceback
            self.insert('audit', messages)

def sanitize(obj):
    '''Remove certain fields from an object to make the display more human readable'''
    excluded_fields = ['date_epoch', 'audit_increment', 'snooze_user']
    fields_to_remove = []
    for field in obj.keys():
        if field.startswith('_') or field in excluded_fields:
            fields_to_remove.append(field)
    for field in fields_to_remove:
        obj.pop(field, None)
    return obj

EMPTY_VALUES = ["", [], {}]

AuditSummary = Dict[str, Literal['added', 'removed', 'updated']]

def diff_summary(old: dict, new: dict) -> AuditSummary:
    '''Return a summary of the diff'''
    field_dict = {}
    fields = set.union(set(old.keys()), set(new.keys()))
    for field in fields:
        old_field, new_field = old.get(field), new.get(field)
        if old_field != new_field:
            if old_field is None:
                field_dict[field] = 'added'
            elif new_field is None:
                field_dict[field] = 'removed'
            elif old_field in EMPTY_VALUES:
                field_dict[field] = 'added'
            elif new_field in EMPTY_VALUES:
                field_dict[field] = 'removed'
            else:
                field_dict[field] = 'updated'
    return field_dict
