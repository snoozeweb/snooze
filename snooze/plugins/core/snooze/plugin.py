import sys

from snooze.plugins.core import Plugin, Abort_and_write
from snooze.utils import Condition
from dateutil import parser

from collections import defaultdict

from logging import getLogger
from datetime import datetime, timedelta, time
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
        time_constraints = snooze.get('time_constraints', [])
        constraints = []
        for time_constraint in time_constraints:
            obj = Constraint.detect(time_constraint)
            constraints.append(obj)
        self.time_constraint = MultiConstraint(*constraints)

    def match(self, record):
        '''Whether a record match the Snooze object'''
        record_date = get_record_date(record)
        return self.condition.match(record) and self.time_constraint.match(record_date)

def get_record_date(record):
    '''Extract the date of the record and return a `datetime` object'''
    if record.get('timestamp'):
        record_date = parser.parse(record['timestamp']).astimezone()
    elif record.get('date_epoch'):
        record_date = datetime.fromtimestamp(record['date_epoch']).astimezone()
    else:
        record_date = datetime.now().astimezone()
    return record_date

class MultiConstraint:
    def __init__(self, *constraints):
        self.constraints_by_type = defaultdict(list)
        for constraint in constraints:
            class_name = constraint.__class__.__name__
            self.constraints_by_type[class_name].append(constraint)

    def match(self, record_date):
        '''
        Match all constraints, but make sure constraints of the same
        type are merged with `OR`.
        '''
        return all(
            any(constraint.match(record_date) for constraint in constraints)
            for _, constraints in self.constraints_by_type.items()
        )

class Constraint:
    @staticmethod
    def detect(time_constraint_dict):
        '''Return the correct time constraint object given a dictionary representing it'''
        constraint_type = time_constraint_dict.pop('type')

        if constraint_type is None:
            return ForeverConstraint()

        try:
            class_obj = getattr(sys.modules[__name__], constraint_type)
            if issubclass(class_obj, Constraint):
                return class_obj(**time_constraint_dict)
            else:
                log.error("Constraint type %s does not inherit from Contraint", constraint_type)
                raise Exception("Constraint type %s does not inherit from Contraint" % constraint_type)
        except AttributeError:
            log.error("No such constraint type: %s", constraint_type)
            raise Exception("No such constraint type: %s" % constraint_type)

    def match(self, _record_date):
        '''Method to fill when inheriting this class'''
        pass

class ForeverConstraint(Constraint):
    '''Always match'''
    def __init__(self, **kwargs):
        pass
    def match(self, _):
        return True

class DatetimeConstraint(Constraint):
    '''
    A time constraint using fixed dates.
    Features:
        * Before a fixed date
        * After a fixed date
        * Between two fixed dates
    '''
    def __init__(self, date_from=None, date_until=None):
        self.date_from = parser.parse(date_from).astimezone() if date_from else None
        self.date_until = parser.parse(date_until).astimezone() if date_until else None
    def match(self, record_date):
        '''Perform a fixed date matching'''
        date_from = self.date_from
        date_until = self.date_until
        if date_from and date_until:
            return (date_from < record_date) and (record_date < date_until)
        elif (not date_from) and date_until:
            return record_date < date_until
        elif date_from and (not date_until):
            return date_from < record_date
        else:
            return False

class WeekdayConstraint(Constraint):
    '''
    Features:
        * Match certain days of the week
    '''
    def __init__(self, weekdays=list):
        self.weekdays = weekdays
    def match(self, record_date):
        weekday_number = int(record_date.strftime('%w'))
        return weekday_number in self.weekdays

class TimeConstraint(Constraint):
    '''
    A time constraint that has a daily period.
    Features:
        * Match before/after/between fixed hours
    '''
    def __init__(self, time_from=None, time_until=None):
        self.time_from = parser.parse(time_from).astimezone().time() if time_from else None
        self.time_until = parser.parse(time_until).astimezone().time() if time_until else None
    def match(self, record_date):
        '''Match a daily periodic time constraint'''
        time_from = self.time_from
        time_until = self.time_until
        record_time = record_date.time()
        if time_from and time_until:
            return (time_from < record_time) and (record_time < time_until)
        elif time_from and (not time_until):
            return time_from < record_time
        elif (not time_from) and time_until:
            return record_time < time_until
        else:
            return True
