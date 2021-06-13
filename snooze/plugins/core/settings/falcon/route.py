#!/usr/bin/python
import os
import json
import falcon
from bson.json_util import loads, dumps
from bson.errors import BSONError
from json import JSONDecodeError
from logging import getLogger
log = getLogger('snooze.api')

from snooze.api.base import BasicRoute
from snooze.utils import config
from snooze.api.falcon import authorize

class SettingsRoute(BasicRoute):
    @authorize
    def on_get(self, req, resp, conf=''):
        c = req.params.get('c') or conf
        log.debug("Loading config file {}".format(c))
        result_dict = config(c)
        resp.content_type = falcon.MEDIA_JSON
        if result_dict:
            result_dict = {k:v for k,v in result_dict.items() if 'password' not in k}
            result = dumps({'data': [result_dict], 'count': 1})
            resp.body = result
            if 'error' in result_dict.keys():
                resp.status = falcon.HTTP_503
            else:
                resp.status = falcon.HTTP_200
        else:
            resp.body = '{}'
            resp.status = falcon.HTTP_404
            pass

    @authorize
    def on_put(self, req, resp, conf=''):
        c = req.params.get('c') or conf
        resp.content_type = falcon.MEDIA_JSON
        try:
            log.debug("Trying write to configfile {}: {}".format(c, req.media))
            media = req.media[0].copy()
            media_config = {k:v for k,v in media.get('conf', {}).items() if ('password' not in k) or v}
            results = self.api.write_and_reload(c, media_config, media.get('reload'), True)
            result = dumps({'data': results.get('text', '')})
            resp.body = result
            resp.status = results.get('status', falcon.HTTP_503)
        except Exception as e:
            log.exception(e)
            resp.body = '{}'
            resp.status = falcon.HTTP_404
            pass
