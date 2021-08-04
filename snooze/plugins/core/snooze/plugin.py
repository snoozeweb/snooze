from snooze.plugins.core import Plugin, Abort_and_write
from snooze.utils import Condition
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

        # Initializing the time constraints
        log.debug("Init Snooze filter {} Time Constraints".format(self.name))
        self.time_constraint = init_time_constraints(snooze.get('time_constraints', {}))

    def match(self, record):
        '''Whether a record match the Snooze object'''
        return self.condition.match(record) and self.time_constraint.match(get_record_date(record))
