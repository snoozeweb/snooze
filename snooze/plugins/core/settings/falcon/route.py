#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

import hashlib
from logging import getLogger

import falcon
from pydantic import ValidationError

from snooze.api.routes import BasicRoute
from snooze.utils.functions import authorize

log = getLogger('snooze.api')

class SettingsRoute(BasicRoute):
    @authorize
    def on_get(self, req, resp, conf=''):
        '''Fetch a config file data.
        Secrets are protected thanks to the `Field(exclude=True)` of pydantic.
        ValidationError are server side errors (the local config file is broken)'''

        resp.content_type = falcon.MEDIA_JSON

        section = req.params.get('c') or conf
        log.debug("Loading config file %s", section)
        try:
            config = self.core.config[section]
            resp.media = {'data': config.dict()}
            resp.status = falcon.HTTP_OK
        except KeyError:
            raise falcon.HTTPNotFound(description=f"Unknown config section '{section}'")
        except ValidationError as err:
            raise falcon.HTTPInternalServerError(
                description=f"Config section '{section}' is invalid on the server: {err}") from err

    @authorize
    def on_put(self, req, resp, conf=''):
        '''Rewrite a setting section on the server'''

        resp.content_type = falcon.MEDIA_JSON

        section = req.params.get('c') or conf
        media = req.media[0].copy()
        results = self.api.write_and_reload(section, media, True)
        result = {'data': results.get('text', '')}
        resp.media = result
        resp.status = results.get('status', falcon.HTTP_503)
