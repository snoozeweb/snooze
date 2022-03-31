#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''Cusotm falcon routes for the snooze core plugin'''

from logging import getLogger

import falcon

from snooze.api.falcon import authorize
from snooze.plugins.core.basic.falcon.route import Route
from snooze.utils.condition import get_condition

log = getLogger('snooze.api')

class SnoozeRoute(Route):
    '''Pre-process the time_constraints field for sorting purposes on the web interface'''
    @authorize
    def on_post(self, req, resp):
        self.pre_process(req)
        super(SnoozeRoute, self).on_post(req, resp)

    @authorize
    def on_put(self, req, resp):
        self.pre_process(req)
        super(SnoozeRoute, self).on_put(req, resp)

    def pre_process(self, req):
        '''Pre-process the object before it being inserted in the database'''
        if not isinstance(req.media, list):
            req.media['sort'] = self.get_date(req.media.get('time_constraints', {}))
        else:
            for req_media in req.media:
                req_media['sort'] = self.get_date(req_media.get('time_constraints', {}))

    def get_date(self, time_constraints):
        '''Get a hint used for sorting'''
        for date_obj in time_constraints.get('datetime', []):
            return 'a_'+date_obj.get('until', '')
        for date_obj in time_constraints.get('time', []):
            return 'b_'+date_obj.get('until', '')
        return 'c'

class SnoozeApplyRoute(Route):
    '''Route used for executing the snooze retro-appy functionality'''
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
        except Exception as err:
            log.exception(err)
            had_errors = True
        if had_errors:
            resp.status = falcon.HTTP_503
        else:
            resp.status = falcon.HTTP_200
        resp.text = str(count)
