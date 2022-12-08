#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

from logging import getLogger
from urllib.parse import unquote

import falcon
from falcon import Request, Response
import bson.json_util

from snooze.api.routes import FalconRoute
from snooze.utils.functions import authorize
from snooze.utils.typing import AuthPayload

log = getLogger('snooze-api')

class ProfileRoute(FalconRoute):
    @authorize
    def on_get(self, req: Request, resp: Response, section: str):
        if 'uid' in req.params:
            query = ['=', 'uid', req.params['uid']]
        elif 'name' in req.params and 'method' in req.params:
            query = ['AND', ['=', 'name', req.params['name']], ['=', 'method', req.params['method']]]
        else:
            query = req.params.get('s')
            try:
                query = bson.json_util.loads(unquote(query))
            except Exception:
                pass
        if self.options.inject_payload:
            query = self.inject_payload_search(req, query)
        log.debug("Loading profile %s", section)
        result_dict = self.search(f"profile.{section}", query)
        resp.content_type = falcon.MEDIA_JSON
        if result_dict:
            try:
                resp.media = result_dict
                resp.status = falcon.HTTP_200
            except Exception as err:
                raise falcon.HTTPInternalServerError(description=f"{err}") from err
        else:
            resp.media = {}
            resp.status = falcon.HTTP_200

    @authorize
    def on_put(self, req: Request, resp: Response, section: str):
        if self.options.inject_payload:
            self.inject_payload_media(req, resp)
        resp.content_type = falcon.MEDIA_JSON
        try:
            if section == 'general':
                self.update_password(req.media)
            result_dict = self.update(f"profile.{section}", req.media)
            resp.media = result_dict
            resp.status = falcon.HTTP_201
        except Exception as err:
            log.exception(err)
            resp.media = {}
            resp.status = falcon.HTTP_503

    @authorize
    def on_delete(self, req: Request, resp: Response, section: str):
        if 'uid' in req.params:
            query = ['=', 'uid', req.params['uid']]
        elif 'name' in req.params and 'method' in req.params:
            query = ['AND', ['=', 'name', req.params['name']], ['=', 'method', req.params['method']]]
        else:
            query = req.params.get('s')
            try:
                query = bson.json_util.loads(query)
            except Exception:
                pass
        if self.options.inject_payload:
            query = self.inject_payload_search(req, query)
        log.debug("Trying delete profile %s", section)
        result_dict = self.delete(f"profile.{section}", query)
        resp.content_type = falcon.MEDIA_JSON
        if result_dict:
            try:
                resp.media = result_dict
                resp.status = falcon.HTTP_OK
            except Exception:
                resp.media = {}
                resp.status = falcon.HTTP_503
        else:
            resp.media = {}
            resp.status = falcon.HTTP_NOT_FOUND
