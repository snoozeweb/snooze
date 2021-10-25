#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

#!/usr/bin/python
from logging import getLogger
log = getLogger('snooze.api')

from snooze.plugins.core.basic.falcon.route import Route
from snooze.api.falcon import authorize

class UserRoute(Route):
    @authorize
    def on_post(self, req, resp):
        for req_media in req.media:
            req_media['method'] = 'local'
            self.update_password(req_media)
        super(UserRoute, self).on_post(req, resp)

    @authorize
    def on_put(self, req, resp):
        for req_media in req.media:
            self.update_password(req_media)
        super(UserRoute, self).on_put(req, resp)
