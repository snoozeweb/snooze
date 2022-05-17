#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

import falcon
from pydantic import ValidationError

from snooze.api.routes import BasicRoute
from snooze.utils.functions import authorize

class SettingsRoute(BasicRoute):
    '''Route for fetching and modifying writable config files (settings sections)'''

    @authorize
    def on_get(self, _req, resp, section: str):
        '''Fetch a config file data.
        Secrets are protected thanks to the `Field(exclude=True)` of pydantic.
        ValidationError are server side errors (the local config file is broken)'''

        resp.content_type = falcon.MEDIA_JSON
        try:
            config = getattr(self.core.config, section)
            resp.media = {'data': config.dict()}
            resp.status = falcon.HTTP_OK
        except AttributeError as err:
            raise falcon.HTTPNotFound(description=f"Unknown config section '{section}'") from err
        except ValidationError as err:
            raise falcon.HTTPInternalServerError(
                description=f"Config section '{section}' is invalid on the server: {err}") from err

    @authorize
    def on_put(self, req, resp, section: str):
        '''Rewrite a setting section on the server'''
        resp.content_type = falcon.MEDIA_JSON
        propagate = (req.params.get('propagate') is not None) # Key existence

        try:
            config = getattr(self.core.config, section)
        except AttributeError as err:
            raise falcon.HTTPNotFound(description=f"Unknown config section '{section}'") from err
        except ValidationError as err:
            raise falcon.HTTPInternalServerError(
                description=f"Config section '{section}' is invalid on the server: {err}") from err
        try:
            config.update(req.media)
        except AttributeError as err:
            raise falcon.HTTPNotFound(description=f"Config section not writable: '{section}'") from err
        except ValidationError as err:
            raise falcon.HTTPBadRequest(
                description=f"Validation error in setting section '{section}': {err}") from err
        for auth in config._auth_routes:
            auth_route = self.api.auth_routes.get(auth)
            if auth_route:
                auth_route.reload()
        if propagate:
            print(req.headers)
            print(req.context)
            self.core.sync_setting_update(section, req.media, req.get_header('Authorization'))
            resp.status = falcon.HTTP_ACCEPTED
        else:
            resp.status = falcon.HTTP_OK
