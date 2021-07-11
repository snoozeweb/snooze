#!/usr/bin/python

from snooze.plugins.core.basic.falcon.route import Route
from snooze.api.falcon import authorize
from logging import getLogger
log = getLogger('snooze.api')

class SnoozeRoute(Route):
    @authorize
    def on_post(self, req, resp):
        for req_media in req.media:
            req_media['sort'] = self.get_date(req_media.get('time_constraints', {}))
        super(SnoozeRoute, self).on_post(req, resp)

    @authorize
    def on_put(self, req, resp):
        for req_media in req.media:
            req_media['sort'] = self.get_date(req_media.get('time_constraints', {}))
        super(SnoozeRoute, self).on_put(req, resp)

    def get_date(self, time_constraints):
        for date_obj in time_constraints.get('datetime', []):
            return 'a_'+date_obj.get('until', '')
        for date_obj in time_constraints.get('time', []):
            return 'b_'+date_obj.get('until', '')
        return 'c'
