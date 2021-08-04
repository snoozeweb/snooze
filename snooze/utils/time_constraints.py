#!/usr/bin/python3.6
import sys

from dateutil import parser
from collections import defaultdict
from datetime import datetime, timedelta, time

from logging import getLogger
log = getLogger('snooze.time_constraints')

def get_record_date(record):
    '''Extract the date of the record and return a `datetime` object'''
    if record.get('timestamp'):
        record_date = parser.parse(record['timestamp']).astimezone()
    elif record.get('date_epoch'):
        record_date = datetime.fromtimestamp(record['date_epoch']).astimezone()
    else:
        record_date = datetime.now().astimezone()
    return record_date

def init_time_constraints(time_constraints):
    constraints = []
    log.debug("Init Time Constraints with {}".format(time_constraints))
    for constraint_type in time_constraints:
        ctype = constraint_type
        try:
            if constraint_type == 'datetime':
                ctype = 'DateTimeConstraint'
            elif constraint_type == 'time':
                ctype = 'TimeConstraint'
            elif constraint_type == 'weekdays':
                ctype = 'WeekdaysConstraint'
            class_obj = getattr(sys.modules[__name__], ctype)
            if issubclass(class_obj, Constraint):
                for constraint_data in time_constraints.get(constraint_type, []):
                    log.debug("Time Constraint {} detected. Data: {}".format(ctype, constraint_data))
                    constraints.append(class_obj(constraint_data))
            else:
                log.error("Constraint type %s does not inherit from Contraint", ctype)
                raise Exception("Constraint type %s does not inherit from Contraint" % ctype)
        except Exception as e:
            log.exception(e)
    return MultiConstraint(*constraints)

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
    def match(self, _record_date):
        '''Method to fill when inheriting this class'''
        pass

class DateTimeConstraint(Constraint):
    '''
    A time constraint using fixed dates.
    Features:
        * Before a fixed date
        * After a fixed date
        * Between two fixed dates
    '''
    def __init__(self, content={}):
        date_from = content.get('from')
        date_until = content.get('until')
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

class WeekdaysConstraint(Constraint):
    '''
    Features:
        * Match certain days of the week
    '''
    def __init__(self, content={}):
        self.weekdays = content.get('weekdays', [])
    def match(self, record_date):
        weekday_number = int(record_date.strftime('%w'))
        return weekday_number in self.weekdays

class TimeConstraint(Constraint):
    '''
    A time constraint that has a daily period.
    Features:
        * Match before/after/between fixed hours
    '''
    def __init__(self, content={}):
        time_from = content.get('from')
        time_until = content.get('until')
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
