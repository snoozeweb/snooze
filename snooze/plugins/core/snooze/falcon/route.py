#!/usr/bin/python

from snooze.plugins.core.basic.falcon.route import Route
from snooze.api.falcon import authorize
from logging import getLogger
log = getLogger('snooze.api')

class SnoozeRoute(Route):
    @authorize
    def on_post(self, req, resp):
        for req_media in req.media:
            req_media['sort'] = self.get_date(req_media.get('time_constraint', []))
            req_media['time_constraints'] = self.get_constraints(req_media.get('time_constraint', []))
        super(SnoozeRoute, self).on_post(req, resp)

    @authorize
    def on_put(self, req, resp):
        for req_media in req.media:
            req_media['sort'] = self.get_date(req_media.get('time_constraint', []))
            req_media['time_constraints'] = self.get_constraints(req_media.get('time_constraint', []))
        super(SnoozeRoute, self).on_put(req, resp)

    def get_date(self, time_constraint):
        for date_obj in time_constraint:
            if date_obj.get('type') == 'DateTime':
                return 'a_'+date_obj.get('content', {}).get('until', '')
        for date_obj in time_constraint:
            if date_obj.get('type') == 'Time':
                return 'b_'+date_obj.get('content', {}).get('until', '')
        return 'c'

    def get_constraints(self, time_constraint):
        datetime_constraints = []
        time_constraints = []
        weekdays_constraints = []
        for date_obj in time_constraint:
            if date_obj.get('type') == 'DateTime':
                datetime_constraints.append(date_obj.get('content', {}))
            elif date_obj.get('type') == 'Time':
                time_constraints.append(date_obj.get('content', {}))
            elif date_obj.get('type') == 'Weekdays':
                for weekday in date_obj.get('content', {}).get('weekdays', []):
                    if weekday not in weekdays_constraints:
                        weekdays_constraints.append(weekday)
        return_hash = {}
        if datetime_constraints:
            return_hash['datetime'] = datetime_constraints
        if time_constraints:
            return_hash['time'] = time_constraints
        if weekdays_constraints:
            return_hash['weekdays'] = weekdays_constraints
        return return_hash
        #return {'datetime': datetime_constraints, 'time': time_constraints, 'weekdays': weekdays_constraints}
