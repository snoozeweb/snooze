#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''A module where the snooze core plugin resides'''

from logging import getLogger

from snooze.db.database import AsyncIncrement
from snooze.plugins.core import Plugin, AbortAndWrite, Abort
from snooze.utils.condition import get_condition, validate_condition
from snooze.utils.time_constraints import get_record_date, init_time_constraints
from snooze.utils.typing import Record, SnoozeFilter

log = getLogger('snooze.plugins.snooze')

class Snooze(Plugin):
    '''The snooze process plugin'''

    def __init__(self, *args, **kwargs):
        Plugin.__init__(self, *args, **kwargs)
        self.hits = AsyncIncrement(self.db, 'snooze', 'hits')
        self.core.threads['asyncdb'].new_increment(self.hits)

    def process(self, record: Record) -> Record:
        log.debug("Processing record %s against snooze filters", record.get('hash', ''))
        for filt in self.filters:
            if filt.enabled and filt.match(record):
                log.debug("Snooze filter %s matched record: %s", filt.name, record.get('hash', ''))
                record['snoozed'] = filt.name
                self.hits.increment({'name': filt.name})
                self.core.stats.inc('alert_snoozed', {'name': filt.name})
                if filt.discard:
                    raise Abort()
                else:
                    raise AbortAndWrite(record)
        return record

    def validate(self, obj: dict):
        '''Validate a snooze object'''
        validate_condition(obj)

    def reload_data(self, sync: bool = False):
        super().reload_data()
        filters = []
        for filt in (self.data or []):
            filters.append(SnoozeObject(filt))
        self.filters = filters
        if sync:
            self.sync_neighbors()

    def retro_apply(self, filter_names):
        '''Retro applying a list of snooze filters'''
        log.debug("Attempting to retro apply snooze filters %s", filter_names)
        filters = [f for f in self.filters if f.name in filter_names]
        count = 0
        for filt in filters:
            if filt.enabled:
                if filt.discard:
                    log.debug("Retro apply discard snooze filter %s", filt.name)
                    results = self.db.delete('record', filt.condition_raw)
                    count += results.get('count', 0)
                else:
                    log.debug("Retro apply snooze filter %s", filt.name)
                    count += self.db.set_fields('record', {'snoozed': filt.name}, filt.condition_raw)
        return count

class SnoozeObject:
    '''Object representing the snooze filter in the database'''
    def __init__(self, snooze: SnoozeFilter):
        self.enabled = snooze.get('enabled', True)
        self.name = snooze['name']
        self.condition = get_condition(snooze.get('condition'))
        self.condition_raw = snooze.get('condition')
        self.hits = snooze.get('hits', True)
        self.discard = snooze.get('discard', False)
        self.raw = snooze

        # Initializing the time constraints
        log.debug("Init Snooze filter %s Time Constraints", self.name)
        self.time_constraint = init_time_constraints(snooze.get('time_constraints', {}))

    def match(self, record: Record) -> bool:
        '''Whether a record match the Snooze object'''
        return self.condition.match(record) and self.time_constraint.match(get_record_date(record))
