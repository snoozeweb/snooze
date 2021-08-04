from snooze.plugins.core.snooze.plugin import *
from snooze.plugins.core import Abort, Abort_and_write

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

