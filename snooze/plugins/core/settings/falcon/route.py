#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

#!/usr/bin/python
import os
import falcon
import hashlib
from logging import getLogger
log = getLogger('snooze.api')

from snooze.api.base import BasicRoute
from snooze.utils import config
from snooze.api.falcon import authorize

class SettingsRoute(BasicRoute):
    @authorize
    def on_get(self, req, resp, conf=''):
        c = req.params.get('c') or conf
        checksum = req.params.get('checksum')
        log.debug("Loading config file {}".format(c))
        result_dict = config(c)
        resp.content_type = falcon.MEDIA_JSON
        if result_dict:
            result_dict = {k:v for k,v in result_dict.items() if 'password' not in k}
            dict_checksum = hashlib.md5(repr([result_dict]).encode('utf-8')).hexdigest()
            if checksum != dict_checksum:
                result = {'data': [result_dict], 'count': 1, 'checksum': dict_checksum}
            else:
                result = {'count': 0}
            resp.media = result
            if 'error' in result_dict.keys():
                resp.status = falcon.HTTP_503
            else:
                resp.status = falcon.HTTP_200
        else:
            resp.media = {}
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
            result = {'data': results.get('text', '')}
            resp.media = result
            resp.status = results.get('status', falcon.HTTP_503)
        except Exception as e:
            log.exception(e)
            resp.media = {}
            resp.status = falcon.HTTP_404
            pass
