#!/usr/bin/python
import os
import json
import falcon
from bson.json_util import loads, dumps
from bson.errors import BSONError
from json import JSONDecodeError
from logging import getLogger
log = getLogger('snooze.api')

from snooze.api.falcon import authorize, FalconRoute

class Route(FalconRoute):
    @authorize
    def on_get(self, req, resp, search='[]', nb_per_page=0, page_number=1, order_by='', asc='true'):
        if 's' in req.params or 'perpage' in req.params or 'orderby' in req.params or 'asc' in req.params:
            s = req.params.get('s') or search
            perpage = req.params.get('perpage') or nb_per_page
            pagenb = req.params.get('pagenb') or page_number
            orderby = req.params.get('orderby') or order_by
            ascending = req.params.get('asc') or asc
        else:
            s = search
            perpage = nb_per_page
            pagenb = page_number
            orderby = order_by
            ascending = asc
        try:
            cond_or_uid = loads(s)
        except:
            cond_or_uid = s
        if self.inject_payload:
            cond_or_uid = self.inject_payload_search(req, cond_or_uid)
        log.debug("Trying search {}".format(cond_or_uid))
        result_dict = self.search(self.plugin.name, cond_or_uid, int(perpage), int(pagenb), orderby, ascending.lower() == 'true')
        resp.content_type = falcon.MEDIA_JSON
        if result_dict:
            result = dumps(result_dict)
            resp.body = result
            resp.status = falcon.HTTP_200
        else:
            resp.body = '{}'
            resp.status = falcon.HTTP_404
            pass

    @authorize
    def on_post(self, req, resp):
        if self.inject_payload:
            self.inject_payload_media(req, resp)
        resp.content_type = falcon.MEDIA_JSON
        try:
            log.debug("Trying to insert {}".format(req.media))
            media = req.media.copy()
            for req_media in media:
                req_media['snooze_user'] = {'name': req.context['user']['user']['name'], 'method': req.context['user']['user']['method']}
            result = dumps(self.insert(self.plugin.name, media))
            resp.body = result
            self.plugin.reload_data(True)
            resp.status = falcon.HTTP_201
        except:
            resp.body = []
            resp.status = falcon.HTTP_503
            pass

    @authorize
    def on_put(self, req, resp):
        if self.inject_payload:
            self.inject_payload_media(req, resp)
        resp.content_type = falcon.MEDIA_JSON
        try:
            log.debug("Trying to update {}".format(req.media))
            media = req.media.copy()
            result = dumps(self.update(self.plugin.name, media))
            resp.body = result
            self.plugin.reload_data(True)
            resp.status = falcon.HTTP_201
        except:
            resp.body = []
            resp.status = falcon.HTTP_503
            pass

    @authorize
    def on_delete(self, req, resp, search='[]'):
        if 'uid' in req.params:
            cond_or_uid = ['=', 'uid', req.params['uid']]
        else:
            string = req.params.get('s') or search
            try:
                cond_or_uid = loads(string)
            except:
                cond_or_uid = string
        if self.inject_payload:
            cond_or_uid = self.inject_payload_search(req, cond_or_uid)
        log.debug("Trying delete %s" % cond_or_uid)
        result_dict = self.delete(self.plugin.name, cond_or_uid)
        resp.content_type = falcon.MEDIA_JSON
        if result_dict:
            result = dumps(result_dict)
            resp.body = result
            self.plugin.reload_data(True)
            resp.status = falcon.HTTP_OK
        else:
            resp.body = '{}'
            resp.status = falcon.HTTP_NOT_FOUND
            pass
