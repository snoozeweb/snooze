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
            'time_constraints': [
                {'type': 'WeekdayConstraint', 'weekdays': [1,2,3,4]},
                {'type': 'TimeConstraint', 'time_from': '10:00', 'time_until': '14:00'}
            ],
        }
        assert SnoozeObject(snooze).match(record) == True

class TestMultiConstraint:

    def test_match_true(self):
        record_date = parser.parse('2021-07-01T12:00:00+09:00')
        c1 = DatetimeConstraint(date_until='2021-07-01T14:30:00+09:00')
        c2 = WeekdayConstraint(weekdays=[1,2,3,4])
        c = MultiConstraint(c1, c2)
        assert c.match(record_date) == True

    def test_match_false(self):
        record_date = parser.parse('2021-07-01T12:00:00+09:00')
        c1 = DatetimeConstraint(date_until='2021-07-01T14:30:00+09:00')
        c2 = WeekdayConstraint(weekdays=[6,7])
        c = MultiConstraint(c1, c2)
        assert c.match(record_date) == False

    def test_match_any_same_type(self):
        record_date = parser.parse('2021-07-01T23:00:00+09:00')
        c1 = TimeConstraint(time_from='00:00', time_until='02:00')
        c2 = TimeConstraint(time_from='22:00', time_until='23:59')
        c = MultiConstraint(c1, c2)
        assert c.match(record_date) == True

class TestDatetimeConstraint:

    def test_until_true(self):
        record_date = parser.parse('2021-07-01T12:00:00+09:00')
        tc = DatetimeConstraint(date_until='2021-07-01T14:30:00+09:00')
        assert tc.match(record_date) == True

    def test_until_false(self):
        record_date = parser.parse('2021-07-01T12:00:00+09:00')
        tc = DatetimeConstraint(date_until='2021-07-01T11:30:00+09:00')
        assert tc.match(record_date) == False

    def test_from_true(self):
        record_date = parser.parse('2021-07-01T12:00:00+09:00')
        tc = DatetimeConstraint(date_from='2021-07-01T10:30:00+09:00')
        assert tc.match(record_date) == True

    def test_from_false(self):
        record_date = parser.parse('2021-07-01T12:00:00+09:00')
        tc = DatetimeConstraint(date_from='2021-07-01T12:30:00+09:00')
        assert tc.match(record_date) == False

class TestForeverConstraint:

    def test_forever(self):
        record_date = parser.parse('2021-07-01T12:00:00+09:00')
        tc = ForeverConstraint()
        assert tc.match(record_date) == True

class TestWeekdayConstraint:

    def test_weekday_true(self):
        record_date = parser.parse('2021-07-01T12:00:00+09:00') # Thursday (4's day of the week)
        tc = WeekdayConstraint(weekdays=[4])
        assert tc.match(record_date) == True

    def test_weekday_false(self):
        record_date = parser.parse('2021-07-01T12:00:00+09:00') # Thursday (4's day of the week)
        tc = WeekdayConstraint(weekdays=[6, 7])
        assert tc.match(record_date) == False

class TestTimeConstraint:

    def test_from_true(self):
        record_date = parser.parse('2021-07-01T12:00:00+09:00')
        tc = TimeConstraint(time_from='10:00')
        assert tc.match(record_date) == True

    def test_from_false(self):
        record_date = parser.parse('2021-07-01T12:00:00+09:00')
        tc = TimeConstraint(time_from='14:00')
        assert tc.match(record_date) == False

    def test_until_true(self):
        record_date = parser.parse('2021-07-01T12:00:00+09:00')
        tc = TimeConstraint(time_until='14:00')
        assert tc.match(record_date) == True

    def test_until_false(self):
        record_date = parser.parse('2021-07-01T12:00:00+09:00')
        tc = TimeConstraint(time_until='10:00')
        assert tc.match(record_date) == False

    def test_range_true(self):
        record_date = parser.parse('2021-07-01T12:00:00+09:00')
        tc = TimeConstraint(time_from='10:00', time_until='14:00')
        assert tc.match(record_date) == True

    def test_range_false(self):
        record_date = parser.parse('2021-07-01T08:00:00+09:00')
        tc = TimeConstraint(time_from='10:00', time_until='14:00')
        assert tc.match(record_date) == False
