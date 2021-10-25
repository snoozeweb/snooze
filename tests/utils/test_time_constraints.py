#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

from snooze.utils.time_constraints import MultiConstraint, DateTimeConstraint, WeekdaysConstraint, TimeConstraint
from dateutil import parser

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

