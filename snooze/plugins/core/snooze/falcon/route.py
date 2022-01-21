#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

#!/usr/bin/python

import falcon
from snooze.plugins.core.basic.falcon.route import Route
from snooze.api.falcon import authorize
from snooze.utils.condition import get_condition
from logging import getLogger
log = getLogger('snooze.api')

class SnoozeRoute(Route):
    @authorize
    def on_post(self, req, resp):
        self.pre_process(req)
        super(SnoozeRoute, self).on_post(req, resp)

    @authorize
    def on_put(self, req, resp):
        self.pre_process(req)
        super(SnoozeRoute, self).on_put(req, resp)

    def pre_process(self, req):
        if not isinstance(req.media, list):
            req.media['sort'] = self.get_date(req.media.get('time_constraints', {}))
        else:
            for req_media in req.media:
                req_media['sort'] = self.get_date(req_media.get('time_constraints', {}))

    def get_date(self, time_constraints):
        for date_obj in time_constraints.get('datetime', []):
            return 'a_'+date_obj.get('until', '')
        for date_obj in time_constraints.get('time', []):
            return 'b_'+date_obj.get('until', '')
        return 'c'

class SnoozeApplyRoute(Route):
    @authorize
    def on_put(self, req, resp):
        resp.content_type = falcon.MEDIA_JSON
        count = 0
        had_errors = False
        media = req.media.copy()
        if not isinstance(media, list):
            media = [media]
        try:
            count = self.plugin.retro_apply(media)
        except Exception as e:
            log.exception(e)
            had_errors = True
            pass
        if had_errors:
            resp.status = falcon.HTTP_503
        else:
            resp.status = falcon.HTTP_200
        resp.text = str(count)
