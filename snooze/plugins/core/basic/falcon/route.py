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
from uuid import uuid4

from dataclasses import dataclass
from typing_extensions import Literal

import falcon
import bson.json_util

from snooze.api.routes import FalconRoute
from snooze.utils.parser import parser
from snooze.utils.functions import authorize

log = getLogger('snooze-api')

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
        if self.options.inject_payload:
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
        if self.options.inject_payload:
            self.inject_payload_media(req, resp)
        resp.content_type = falcon.MEDIA_JSON
        log.debug("Trying to insert %s", req.media)
        media = req.media.copy()
        if not isinstance(media, list):
            media = [media]
        rejected = []
        validated = []
        for idx, req_media in enumerate(media):
            queries = req_media.get('qls', [])
            req_media['snooze_user'] = {
                'name': req.context.auth.username,
                'method': req.context.auth.method,
            }

            # Validation
            try:
                self._validate(req_media, req, resp)
            except ValidationError:
                rejected.append(req_media)
                continue

            if self.plugin.meta.force_order:
                try:
                    order_query = []
                    if self.plugin.meta.tree and 'parent' in req_media:
                        parent = self.core.db.get_one(self.plugin.name, {'uid': req_media['parent']})
                        if parent:
                            req_media['parents'] = parent.get('parents', []) + [req_media['parent']]
                            order_query = ['OR', ['=', 'uid', req_media['parent']], ['IN', req_media['parent'], 'parents']]
                        else:
                            log.warning('Parent %s does not exist anymore, appending the node to the top level', req_media['parent'])
                            req_media['parents'] = []
                        req_media.pop('parent', None)
                    last_item = self.core.db.search(self.plugin.name, order_query, orderby = self.plugin.meta.force_order, asc = False, only_one = True)
                    if last_item['count'] > 0:
                        last_item = last_item['data'][0]
                        item_order = last_item.get(self.plugin.meta.force_order, 0)
                        self.core.db.inc_many(self.plugin.name, self.plugin.meta.force_order, ['>', self.plugin.meta.force_order, item_order], 1 + idx)
                        for med in validated:
                            if med[self.plugin.meta.force_order] > item_order:
                                 med[self.plugin.meta.force_order] += 1 + idx
                        req_media[self.plugin.meta.force_order] = item_order + 1 + idx
                    else:
                        req_media[self.plugin.meta.force_order] = idx
                except Exception as err:
                    log.exception(err)
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
            self._update_plugin(self.plugin.name)
            resp.status = falcon.HTTP_201
            self._audit(result, req)
        except Exception as err:
            log.exception(err)
            resp.media = []
            resp.status = falcon.HTTP_503

    @authorize
    def on_put(self, req, resp):
        if self.options.inject_payload:
            self.inject_payload_media(req, resp)
        resp.content_type = falcon.MEDIA_JSON
        log.debug("Trying to update %s", req.media)
        media = req.media.copy()
        if not isinstance(media, list):
            media = [media]
        rejected = []
        validated = []
        dragged = []
        for idx, req_media in enumerate(media):
            try:
                self._validate(req_media, req, resp)
            except ValidationError:
                rejected.append(req_media)
                continue

            if self.plugin.meta.force_order:
                try:
                    pivot = None
                    modifier = None
                    if req_media.get('insert_before'):
                        pivot =  self.core.db.get_one(self.plugin.name, {'uid': req_media['insert_before']})
                        modifier = -1
                    elif req_media.get('insert_after'):
                        pivot =  self.core.db.get_one(self.plugin.name, {'uid': req_media['insert_after']})
                        modifier = 0
                    elif self.plugin.meta.tree and 'parent' in req_media:
                        log.error('Parent for %s has been set up while insert_before or insert_after was not. Please set either one of them',
                            req_media)
                        rejected.append(req_media)
                        continue
                    if modifier is not None:
                        log.debug("Initiated drag using pivot pivot %s", pivot)
                        req_media.pop('insert_before', None)
                        req_media.pop('insert_after', None)
                        if not pivot:
                            log.error('Cannot find pivot uid %s. Aborting on_put',
                                req_media.get('insert_before', req_media.get('insert_after')))
                            rejected.append(req_media)
                            continue
                        old_req_media = self.core.db.get_one(self.plugin.name, {'uid': req_media['uid']})
                        if not old_req_media:
                            log.error('Cannot find uid %s. Aborting on_put', req_media.get('uid'))
                            rejected.append(req_media)
                            continue
                        familly_query = ['=', 'uid', req_media['uid']]
                        if self.plugin.meta.tree:
                            familly_query = ['OR', ['=', 'uid', req_media['uid']], ['IN', req_media['uid'], 'parents']]
                            if old_req_media.get('parents'):
                                log.debug("Removing nodes %s from node parents matching %s", old_req_media['parents'], familly_query)
                                self.core.db.remove_list(self.plugin.name, {'parents': old_req_media['parents']}, familly_query)
                            if 'parent' in req_media:
                                parent = self.core.db.get_one(self.plugin.name, {'uid': req_media['parent']})
                                req_media['parents'] = [req_media['parent']]
                                if parent:
                                    req_media['parents'] = parent.get('parents', []) + req_media['parents']
                                if req_media.get('parents'):
                                    log.debug("Prepending nodes %s to node parents matching %s", req_media['parents'], familly_query)
                                    self.core.db.prepend_list(self.plugin.name, {'parents': req_media['parents']}, familly_query)
                                req_media.pop('parent', None)
                        last_item = self.core.db.search(self.plugin.name, familly_query, orderby = self.plugin.meta.force_order, asc = False, only_one = True)['data'][0]
                        last_item_order = last_item.get(self.plugin.meta.force_order, 0)
                        pivot_order = pivot.get(self.plugin.meta.force_order, 0)
                        if last_item_order > pivot_order:
                            log.debug("Pivoting dragging nodes backward")
                            self.core.db.inc_many(self.plugin.name, self.plugin.meta.force_order, ['AND', ['>', self.plugin.meta.force_order, pivot_order + modifier], ['NOT', familly_query]], last_item_order - pivot_order - modifier)
                        else:
                            log.debug("Pivoting dragging nodes forward")
                            self.core.db.inc_many(self.plugin.name, self.plugin.meta.force_order, ['OR', ['>', self.plugin.meta.force_order, pivot_order + modifier], familly_query], pivot_order - old_req_media.get(self.plugin.meta.force_order, 0) + modifier + 1)
                            req_media[self.plugin.meta.force_order] += pivot_order - old_req_media.get(self.plugin.meta.force_order, 0) + modifier + 1
                        dragged.append(req_media)
                        continue
                except Exception as err:
                    log.exception(err)
                    rejected.append(req_media)
                    continue

            validated.append(req_media)
        try:
            if len(dragged) == 0:
                result = self.update(self.plugin.name, validated)
            else:
                result = {'data': {'updated': dragged, 'rejected': []}}
            result['data']['rejected'] += rejected
            resp.media = result
            self._update_plugin(self.plugin.name)
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
        if self.options.inject_payload:
            cond_or_uid = self.inject_payload_search(req, cond_or_uid)
        to_delete = self.search(self.plugin.name, cond_or_uid)
        deleted_objects = [{'_old': data} for data in to_delete['data']]
        if self.plugin.meta.tree and to_delete['count'] > 0:
            self.core.db.delete(self.plugin.name, ['IN', list(set(list(map(lambda x: x['uid'], to_delete['data'])))), 'parents'])
        log.debug("Trying delete %s", cond_or_uid)
        result_dict = self.delete(self.plugin.name, cond_or_uid)
        resp.content_type = falcon.MEDIA_JSON
        self._audit({'data': {'deleted': deleted_objects}}, req)
        if result_dict:
            result = result_dict
            resp.media = result
            self._update_plugin(self.plugin.name)
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

    def _update_plugin(self, name):
        '''Update a plugin data for sync purpose via syncer'''
        latest = self.core.db.get_one('syncer_latest', dict(type='plugin', name=name))
        if latest and latest.get('uid'):
            self.core.db.update_one('syncer_latest', latest.get('uid'), {
                'node': self.core.config.syncer.hostname, # Used for debugging
                'type': 'plugin',
                'timestamp': datetime.now().timestamp(),
            })
        else:
            # The latest should be bootstrapped by reload_data() of each plugin, so this case should never arise.
            self.core.db.replace_one('syncer_latest', dict(type='plugin', name=name), {
                'uid': str(uuid4()),
                'node': f"{self.core.config.syncer.hostname}+bootstrap",
                'type': 'plugin',
                'name': name,
                'timestamp': datetime.now().timestamp(),
            })
            # There is a limitation to this though. There will be a race condition the first time, if two updates
            # happen at the same time. In that case, the update will still be signaled, even if the
            # timestamp will be slightly incorrect. Though, we're already in a unlikely case, so this is good enough.
            # We will add a warning to debug in case we're hitting this unexpectedly.
            log.warning("Unlikely situation happened. The syncer_latest for {type=plugin, name=%s} doesn't exit. \
                It should have been created by reload_data(). If you see this, you might have manually modified your DB, \
                or something unexpected happened. Will bootstrap the value to fix the problem.", name)

    def _audit(self, results, req):
        '''Audit the changed objects in a dedicated collection'''
        if self.plugin.meta.audit:
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
                            username = req.context.auth.username
                            method = req.context.auth.method
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
    excluded_fields = ['date_epoch', 'snooze_user']
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
