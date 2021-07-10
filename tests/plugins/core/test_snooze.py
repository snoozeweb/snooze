from snooze.plugins.core.snooze.plugin import *
from snooze.plugins.core import Abort, Abort_and_write

from datetime import datetime
from dateutil import parser

import pytest

class TestSnooze():
    @pytest.fixture
    def snooze(self, core, config):
        filters = [
            {'name': 'Filter 1', 'condition': ['=', 'a', '1']}
        ]
        core.db.write('snooze', filters)
        return Snooze(core, config)

    def test_snooze_1(self, snooze):
        record = {'a': '1', 'b': '2'}
        try:
            record = snooze.process(record)
            assert False
        except Abort_and_write:
            assert record['snoozed']
    def test_snooze_2(self, snooze):
        record = {'a': '2', 'b': '2'}
        try:
            record = snooze.process(record)
            assert True
        except Abort_and_write:
            assert False

class TestSnoozeObject:

    def test_match_true(self):
        record = {'timestamp': '2021-07-01T12:00:00+09:00', 'host': 'myhost01', 'message': 'my message'}
        snooze = {
            'name': 'Snooze rule 1',
            'condition': ['=', 'host', 'myhost01'],
            'time_constraint': [
                {'type': 'Weekdays', 'content': {'weekdays': [1,2,3,4]}},
                {'type': 'Time', 'content': {'from': '10:00', 'until': '14:00'}}
            ],
        }
        assert SnoozeObject(snooze).match(record) == True

class TestMultiConstraint:

    def test_match_true(self):
        record_date = parser.parse('2021-07-01T12:00:00+09:00')
        c1 = DateTimeConstraint({'until':'2021-07-01T14:30:00+09:00'})
        c2 = WeekdaysConstraint({'weekdays':[1,2,3,4]})
        c3 = TimeConstraint({'from': '11:00', 'until':'15:00'})
        c = MultiConstraint(c1, c2, c3)
        assert c.match(record_date) == True

    def test_match_false(self):
        record_date = parser.parse('2021-07-01T12:00:00+09:00')
        c1 = DateTimeConstraint({'until':'2021-07-01T14:30:00+09:00'})
        c2 = WeekdaysConstraint({'weekdays':[6,7]})
        c = MultiConstraint(c1, c2)
        assert c.match(record_date) == False

    def test_match_any_same_type(self):
        record_date = parser.parse('2021-07-01T23:00:00+09:00')
        c1 = TimeConstraint({'from': '00:00', 'until':'02:00'})
        c2 = TimeConstraint({'from': '22:00', 'until':'23:59'})
        c = MultiConstraint(c1, c2)
        assert c.match(record_date) == True

class TestDateTimeConstraint:

    def test_until_true(self):
        record_date = parser.parse('2021-07-01T12:00:00+09:00')
        tc = DateTimeConstraint({'until':'2021-07-01T14:30:00+09:00'})
        assert tc.match(record_date) == True

    def test_until_false(self):
        record_date = parser.parse('2021-07-01T12:00:00+09:00')
        tc = DateTimeConstraint({'until':'2021-07-01T11:30:00+09:00'})
        assert tc.match(record_date) == False

    def test_from_true(self):
        record_date = parser.parse('2021-07-01T12:00:00+09:00')
        tc = DateTimeConstraint({'from':'2021-07-01T10:30:00+09:00'})
        assert tc.match(record_date) == True

    def test_from_false(self):
        record_date = parser.parse('2021-07-01T12:00:00+09:00')
        tc = DateTimeConstraint({'from':'2021-07-01T12:30:00+09:00'})
        assert tc.match(record_date) == False

class TestWeekdaysConstraint:

    def test_weekday_true(self):
        record_date = parser.parse('2021-07-01T12:00:00+09:00') # Thursday (4's day of the week)
        tc = WeekdaysConstraint({'weekdays':[4]})
        assert tc.match(record_date) == True

    def test_weekday_false(self):
        record_date = parser.parse('2021-07-01T12:00:00+09:00') # Thursday (4's day of the week)
        tc = WeekdaysConstraint({'weekdays':[6,7]})
        assert tc.match(record_date) == False

class TestTimeConstraint:

    def test_from_true(self):
        record_date = parser.parse('2021-07-01T12:00:00+09:00')
        tc = TimeConstraint({'from':'10:00'})
        assert tc.match(record_date) == True

    def test_from_false(self):
        record_date = parser.parse('2021-07-01T12:00:00+09:00')
        tc = TimeConstraint({'from':'14:00'})
        assert tc.match(record_date) == False

    def test_until_true(self):
        record_date = parser.parse('2021-07-01T12:00:00+09:00')
        tc = TimeConstraint({'until':'14:00'})
        assert tc.match(record_date) == True

    def test_until_false(self):
        record_date = parser.parse('2021-07-01T12:00:00+09:00')
        tc = TimeConstraint({'until':'10:00'})
        assert tc.match(record_date) == False

    def test_range_true(self):
        record_date = parser.parse('2021-07-01T12:00:00+09:00')
        tc = TimeConstraint({'from':'10:00', 'until':'14:00'})
        assert tc.match(record_date) == True

    def test_range_false(self):
        record_date = parser.parse('2021-07-01T08:00:00+09:00')
        tc = TimeConstraint({'from':'10:00', 'until':'14:00'})
        assert tc.match(record_date) == False
