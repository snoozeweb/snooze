#!/usr/bin/python3.6

from snooze.plugins.core import Plugin, Abort_and_write
from snooze.utils import Condition
from dateutil import parser

from logging import getLogger
from datetime import datetime, timedelta
log = getLogger('snooze')

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
                raise Abort_and_write
        else:
            return record

    def reload_data(self, sync = False):
        super().reload_data()
        self.filters = []
        for f in (self.data or []):
            self.filters.append(SnoozeObject(f))
        if sync and self.core.cluster:
            self.core.cluster.reload_plugin(self.name)

class SnoozeObject():
    def __init__(self, snooze):
        self.enabled = snooze.get('enabled', True)
        self.name = snooze['name']
        self.condition = Condition(snooze.get('condition'))
        self.hits = snooze.get('hits', True)
        self.raw = snooze
        self.time_constraint = snooze.get('time_constraint', {})
        self.date_epoch = datetime.fromtimestamp(snooze.get('date_epoch', datetime.now().timestamp())).astimezone()
        self.date_from = parser.parse(self.time_constraint.get('from', self.date_epoch.isoformat())).astimezone()
        self.date_until = parser.parse(self.time_constraint.get('until', (self.date_epoch + timedelta(hours=1)).isoformat())).astimezone()
    
    def match(self, record):
        matched_date = True
        if self.time_constraint:
            record_timestamp = record.get('timestamp')
            if record_timestamp:
                record_date = parser.parse(record_timestamp).astimezone()
            else:
                record_epoch = record.get('date_epoch')
                if record_epoch:
                    record_date = datetime.fromtimestamp(record_epoch).astimezone()
                else:
                    record_date = datetime.now().astimezone()
            matched_date = (self.date_from < record_date) and (record_date < self.date_until)
        log.debug("Snooze filter {}. Is record date is between date {} and {}: {}".format(self.name, self.date_from.isoformat(), self.date_until.isoformat(), matched_date))
        return matched_date and self.condition.match(record)

