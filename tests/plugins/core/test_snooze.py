#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

from snooze.plugins.core.snooze.plugin import *
from snooze.plugins.core import Abort, AbortAndWrite

import pytest

class TestSnooze():
    @pytest.fixture
    def snooze(self, core):
        filters = [
            {'name': 'Filter 1', 'condition': ['=', 'a', '1']},
            {'name': 'Filter 2', 'condition': ['=', 'a', '3'], 'discard': True}
        ]
        core.db.delete('snooze', [], True)
        core.db.delete('record', [], True)
        core.db.write('snooze', filters)
        snooze_plugin = Snooze(core)
        snooze_plugin.post_init()
        return snooze_plugin

    def test_snooze_1(self, snooze):
        record = {'a': '1', 'b': '2'}
        try:
            record = snooze.process(record)
            assert False
        except AbortAndWrite:
            assert record['snoozed'] == 'Filter 1'

    def test_snooze_2(self, snooze):
        record = {'a': '2', 'b': '2'}
        try:
            record = snooze.process(record)
            assert True
        except AbortAndWrite:
            assert False

    def test_snooze_3(self, snooze):
        record = {'a': '3', 'b': '2'}
        try:
            record = snooze.process(record)
            assert False
        except Abort:
            assert True

    # $merge not implemented in mongomock
    #def test_retro_apply(self, snooze):
    #    snooze.db.write('record', {'a': '1', 'b': '2'})
    #    count = snooze.retro_apply('Filter 1')
    #    record = snooze.db.search('record')['data'][0]
    #    assert count == 1
    #    assert record['snoozed'] == 'Filter 1'
    
    def test_retro_apply_discard(self, snooze):
        snooze.db.write('record', {'a': '3', 'b': '2'})
        count = snooze.retro_apply(['Filter 2'])
        count_records = snooze.db.search('record')['count']
        assert count == 1
        assert count_records == 0

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

