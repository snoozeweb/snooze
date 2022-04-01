#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''A module for managing time constraint objects, mainly used by the
snooze and notification core plugins'''

import sys
from abc import ABC, abstractmethod
from collections import defaultdict
from datetime import datetime, timedelta
from logging import getLogger
from typing import List, Optional, NewType, Tuple

from dateutil import parser

from snooze.utils.typing import Record

TimeRange = Tuple[datetime, datetime]

log = getLogger('snooze.time_constraints')

def get_record_date(record: Record) -> datetime:
    '''Extract the date of the record and return a `datetime` object'''
    if record.get('timestamp'):
        record_date = parser.parse(record['timestamp']).astimezone()
    elif record.get('date_epoch'):
        record_date = datetime.fromtimestamp(record['date_epoch']).astimezone()
    else:
        record_date = datetime.now().astimezone()
    return record_date

class Constraint(ABC):
    '''A base class for time constraints'''
    @abstractmethod
    def match(self, record_date: datetime) -> bool:
        '''Method to fill when inheriting this class'''
    def __str__(self):
        return "AbstractTimeConstraint"

def init_time_constraints(time_constraints) -> Constraint:
    '''Return a time constraint object from a list of time constraints'''
    constraints = []
    log.debug("Init Time Constraints with %s", time_constraints)
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
                    constraints.append(class_obj(constraint_data))
            else:
                log.error("Constraint type %s does not inherit from Contraint", ctype)
                raise Exception(f"Constraint type {ctype} does not inherit from Contraint")
        except Exception as err:
            log.exception(err)
    return MultiConstraint(*constraints)

class MultiConstraint(Constraint):
    '''An object representing the union of several time constraints'''
    def __init__(self, *constraints: List[Constraint]):
        self.constraints_by_type = defaultdict(list)
        for constraint in constraints:
            class_name = constraint.__class__.__name__
            self.constraints_by_type[class_name].append(constraint)

    def match(self, record_date: datetime) -> bool:
        '''Match all constraints, but make sure constraints of the same
        type are merged with `OR`'''
        return all(
            any(constraint.match(record_date) for constraint in constraints)
            for _, constraints in self.constraints_by_type.items()
        )
    def __str__(self):
        return ' and '.join([
            '(' + ' or '.join([str(constraint) for constraint in constraints]) + ')'
            for _, constraints in self.constraints_by_type.items()
        ])

class DateTimeConstraint(Constraint):
    '''A time constraint using fixed dates.
    Features:
        * Before a fixed date
        * After a fixed date
        * Between two fixed dates
    '''
    def __init__(self, content: Optional[dict] = None):
        if content is None:
            content = {}
        date_from = content.get('from')
        date_until = content.get('until')
        self.date_from = parser.parse(date_from).astimezone() if date_from else None
        self.date_until = parser.parse(date_until).astimezone() if date_until else None
    def match(self, record_date: datetime) -> bool:
        '''Perform a fixed date matching'''
        date_from = self.date_from
        date_until = self.date_until
        if date_from and date_until:
            return date_from <= record_date <= date_until
        elif (not date_from) and date_until:
            return record_date <= date_until
        elif date_from and (not date_until):
            return date_from <= record_date
        else:
            return False
    def __str__(self):
        return f"DateTimeConstraint<{self.date_from} to {self.date_until}>"

class WeekdaysConstraint(Constraint):
    '''A constraint on the days of the week
    Features:
        * Match certain days of the week
    '''
    def __init__(self, content: Optional[dict] = None):
        if content is None:
            content = {}
        self.weekdays = content.get('weekdays', [])
    def match(self, record_date: datetime) -> bool:
        weekday_number = int(record_date.strftime('%w'))
        return weekday_number in self.weekdays
    def __str__(self):
        return f"WeekdaysConstraint<{self.weekdays}>"

class TimeConstraint(Constraint):
    '''A time constraint that has a daily period.
    Features:
        * Match before/after/between fixed hours
        * Support hours over midnight (`from` lower than `until`)
    '''
    def __init__(self, content: Optional[dict] = None):
        if content is None:
            content = {}
        time1 = content.get('from')
        time2 = content.get('until')
        self.time1 = parser.parse(time1).astimezone().timetz() if time1 else None
        self.time2 = parser.parse(time2).astimezone().timetz() if time2 else None

    def get_intervals(self, record_date: datetime) -> List[TimeRange]:
        '''Return the an array of datetime intervals depending on the `from`
        and `until` time, and the date of the record. The intervals will all be
        ordered.'''
        day = timedelta(days=1)
        date1 = datetime.combine(record_date, self.time1)
        date2 = datetime.combine(record_date, self.time2)
        if date2 < date1:
            return [(date1 - day, date2), (date1, date2 + day)]
        return [(date1, date2)]

    def match(self, rd: datetime) -> bool:
        '''Match a daily periodic time constraint.
        rd = record datetime
        '''
        rd = rd.astimezone()
        if self.time1 and self.time2:
            intervals = self.get_intervals(rd.date())
            return any(date1 <= rd <= date2 for date1, date2 in intervals)
        elif self.time1 and not self.time2:
            date1 = datetime.combine(rd.date(), self.time1)
            return date1 <= rd
        elif self.time2 and not self.time1:
            date2 = datetime.combine(rd.date(), self.time2)
            return rd <= date2
        else:
            return True

    def __str__(self):
        return f"TimeConstraint<{self.time1} to {self.time2}>"
