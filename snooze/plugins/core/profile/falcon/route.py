#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

#!/usr/bin/python
import os
import falcon
import bson.json_util
from urllib.parse import unquote
from logging import getLogger
log = getLogger('snooze.api')

from snooze.api.falcon import authorize, FalconRoute

class ProfileRoute(FalconRoute):
    @authorize
    def on_get(self, req, resp, category='', search='[]'):
        if 'uid' in req.params:
            query = ['=', 'uid', req.params['uid']]
        elif 'name' in req.params and 'method' in req.params:
            query = ['AND', ['=', 'name', req.params['name']], ['=', 'method', req.params['method']]]
        else:
            query = req.params.get('s') or search
            try:
                query = bson.json_util.loads(unquote(query))
            except:
                pass
        if self.inject_payload:
            query = self.inject_payload_search(req, query)
        c = req.params.get('c') or category
        log.debug("Loading profile {}".format(c))
        result_dict = self.search(self.plugin.name + '.' + c, query)
        resp.content_type = falcon.MEDIA_JSON
        if result_dict:
            try:
                resp.media = result_dict
                resp.status = falcon.HTTP_200
            except:
                resp.media = {}
                resp.status = falcon.HTTP_503
        else:
            resp.media = {}
            resp.status = falcon.HTTP_404
            pass

    @authorize
    def on_put(self, req, resp, category=''):
        if self.inject_payload:
            self.inject_payload_media(req, resp)
        c = req.params.get('c') or category
        resp.content_type = falcon.MEDIA_JSON
        try:
            media = req.media.copy()
            log.debug("Trying write to profile {}: {}".format(c, media))
            if c == 'general':
                for req_media in media:
                    self.update_password(req_media)
            result_dict = self.update(self.plugin.name + '.' + c, media)
            resp.media = result_dict
            resp.status = falcon.HTTP_201
        except Exception as e:
            log.exception(e)
            resp.media = {}
            resp.status = falcon.HTTP_503
            pass

    @authorize
    def on_delete(self, req, resp, category='', search='[]'):
        if 'uid' in req.params:
            query = ['=', 'uid', req.params['uid']]
        elif 'name' in req.params and 'method' in req.params:
            query = ['AND', ['=', 'name', req.params['name']], ['=', 'method', req.params['method']]]
        else:
            query = req.params.get('s') or search
            try:
                query = bson.json_util.loads(query)
            except:
                pass
        if self.inject_payload:
            query = self.inject_payload_search(req, query)
        c = req.params.get('c') or category
        log.debug("Trying delete profile {}: {}".format(c, media))
        result_dict = self.delete(self.plugin.name + '.' + c, query)
        resp.content_type = falcon.MEDIA_JSON
        if result_dict:
            try:
                resp.media = result_dict
                resp.status = falcon.HTTP_OK
            except:
                resp.media = {}
                resp.status = falcon.HTTP_503
        else:
            resp.media = {}
            resp.status = falcon.HTTP_NOT_FOUND
            pass
