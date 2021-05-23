#!/usr/bin/python3.6

from snooze.plugins.core import Plugin, Abort_and_write
from snooze.utils import Condition

from logging import getLogger
log = getLogger('snooze')

class Snooze(Plugin):
    def process(self, record):
        filters = (self.data or [])
        for f in filters:
            log.debug("Attempting to match {} against filter {}".format(record, f))
            if Condition(f.get('condition')).match(record):
                log.debug("Matched snooze filter {} with {}".format(f['name'], record))
                record['snoozed'] = True
                raise Abort_and_write
        else:
            return record
