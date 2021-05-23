#!/usr/bin/python3.6

from snooze.plugins.core.snooze.plugin import Snooze
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
