#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

from snooze.plugins.core import Plugin, Abort_and_write, Abort
from snooze.utils.condition import get_condition, validate_condition
from snooze.utils.time_constraints import get_record_date, init_time_constraints

from logging import getLogger
log = getLogger('snooze.plugins.snooze')

class Snooze(Plugin):
    def process(self, record):
        for f in self.filters:
            log.debug("Attempting to match {} against snooze filter {}".format(record, f.name))
            if f.enabled and f.match(record):
                log.debug("Matched snooze filter {} with {}".format(f.name, record))
                record['snoozed'] = f.name
                f.hits += 1
                f.raw['hits'] = f.hits
                self.db.write('snooze', f.raw)
                self.core.stats.inc('alert_snoozed', {'name': f.name})
                if f.discard:
                    raise Abort()
                else:
                    raise Abort_and_write(record)
        else:
            return record

    def validate(self, obj):
        '''Validate a snooze object'''
        validate_condition(obj)

    def reload_data(self, sync = False):
        super().reload_data()
        self.filters = []
        for f in (self.data or []):
            self.filters.append(SnoozeObject(f))
        if sync and self.core.cluster:
            self.core.cluster.reload_plugin(self.name)

    def retro_apply(self, filter_names):
        log.debug("Attempting to retro apply snooze filters {}".format(filter_names))
        filters = [f for f in self.filters if f.name in filter_names]
        count = 0
        for f in filters:
            if f.enabled:
                if f.discard:
                    log.debug("Retro apply discard snooze filter {}".format(f.name))
                    results = self.db.delete('record', f.condition_raw)
                    count += results.get('count', 0)
                else:
                    log.debug("Retro apply snooze filter {}".format(f.name))
                    count += self.db.update_fields('record', {'snoozed': f.name}, f.condition_raw)
        return count

class SnoozeObject():
    def __init__(self, snooze):
        self.enabled = snooze.get('enabled', True)
        self.name = snooze['name']
        self.condition = get_condition(snooze.get('condition'))
        self.condition_raw = snooze.get('condition')
        self.hits = snooze.get('hits', True)
        self.discard = snooze.get('discard', False)
        self.raw = snooze

        # Initializing the time constraints
        log.debug("Init Snooze filter {} Time Constraints".format(self.name))
        self.time_constraint = init_time_constraints(snooze.get('time_constraints', {}))

    def match(self, record):
        '''Whether a record match the Snooze object'''
        return self.condition.match(record) and self.time_constraint.match(get_record_date(record))
