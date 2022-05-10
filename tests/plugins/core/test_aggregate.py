#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

#!/usr/bin/python3.6

from snooze.plugins.core.aggregaterule.plugin import Aggregaterule, AggregateruleObject
from snooze.plugins.core import AbortAndUpdate

from logging import getLogger
log = getLogger('snooze.tests')

import pytest

class TestAggregate:
    @pytest.fixture
    def record(self):
        return {'a': '1', 'b': '2'}
    def test_match(self, record):
        aggregate_rule = {'name': 'Agg1', 'condition': ['=', 'a', '1']}
        AggregateruleObject(aggregate_rule).match(record)
        assert record == {'a': '1', 'b': '2', 'aggregate': 'Agg1'}

class TestAggregatePlugin:
    @pytest.fixture
    def aggregateplugin(self, core):
        core.db.delete('record', [], True)
        aggregate_rules = [
            {'name': 'Agg1', 'condition': ['=', 'a', '1'], 'fields': ['a', 'b'], 'throttle': 15},
            {'name': 'Agg2', 'condition': ['=', 'a', '2'], 'fields': ['a', 'b.c'], 'throttle': 15},
            {'name': 'Agg3', 'condition': ['=', 'a', '3'], 'fields': ['a', 'b'], 'throttle': 0},
            {'name': 'Agg4', 'condition': ['=', 'a', '4'], 'fields': ['a', 'b'], 'throttle': 15, 'watch': ['c'], 'flapping': 2},
            {'name': 'Agg5', 'condition': ['=', 'a', '5'], 'fields': ['a', 'b'], 'throttle': 15, 'watch': ['c.test']},
        ]
        core.db.delete('aggregaterule', [], True)
        core.db.write('aggregaterule', aggregate_rules)
        agg_rule = Aggregaterule(core)
        agg_rule.post_init()
        return agg_rule
    def test_agreggate_throttle(self, aggregateplugin):
        records = [
            # Agg1 - 1
            {'a': '1', 'b': '2', 'c': '3'},
            {'a': '1', 'b': '2', 'c': '3'},
            {'a': '1', 'b': '2', 'c': '4'},
            # Agg1 - 2
            {'a': '1', 'b': '0', 'c': '0'},
            # Agg2 - 1
            {'a': '2', 'b': {'c': '4', 'd': '1'}},
            {'a': '2', 'b': {'c': '4', 'd': '2'}},
            # Agg2 - 2
            {'a': '2', 'b': {'c': '3'}},
            # Default
            {'a': '999', 'b': '1', 'c': '3'},
            {'a': '999', 'b': '1', 'c': '3'},
        ]
        for record in records:
            try:
                rec = aggregateplugin.process(record)
                aggregateplugin.core.db.write('record', rec)
            except AbortAndUpdate as e:
                aggregateplugin.core.db.write('record', e.record or record)
                continue
        results1 = aggregateplugin.core.db.search('record', ['=', 'aggregate', 'Agg1'])['data']
        results2 = aggregateplugin.core.db.search('record', ['=', 'aggregate', 'Agg2'])['data']
        results3 = aggregateplugin.core.db.search('record', ['=', 'aggregate', 'default'])['data']
        assert results1[0]['duplicates'] == 3
        assert results1[1]['duplicates'] == 1
        assert results2[0]['duplicates'] == 2
        assert results2[1]['duplicates'] == 1
        assert results3[0]['duplicates'] == 2

    def test_agreggate_nothrottle(self, aggregateplugin):
        records = [
            # Agg3 - 1
            {'a': '3', 'b': '2', 'c': '3'},
            {'a': '3', 'b': '2', 'c': '4'},
        ]
        for record in records:
            rec = aggregateplugin.process(record)
            aggregateplugin.core.db.write('record', rec)
        results = aggregateplugin.core.db.search('record', ['=', 'aggregate', 'Agg3'])['data']
        assert results[0]['duplicates'] == 2
        assert results[0]['comment_count'] == 1

    def test_aggregate_watchedfields(self, aggregateplugin):
        records = [
            # Agg4 - 1
            {'a': '4', 'b': '2', 'c': '3'},
            {'a': '4', 'b': '2', 'c': '4'},
            # Agg5 - 1
            {'a': '5', 'b': '2', 'c': {'test': '3'}},
            {'a': '5', 'b': '2', 'c': {'test': '4'}},
        ]
        for record in records:
            rec = aggregateplugin.process(record)
            aggregateplugin.core.db.write('record', rec)
        results = aggregateplugin.core.db.search('record', ['=', 'aggregate', 'Agg4'])['data']
        assert results[0]['duplicates'] == 2
        assert results[0]['comment_count'] == 1
        results = aggregateplugin.core.db.search('record', ['=', 'aggregate', 'Agg5'])['data']
        assert results[0]['duplicates'] == 2
        assert results[0]['comment_count'] == 1

    def test_aggregate_ok(self, aggregateplugin):
        records = [
            {'a': '1', 'state': 'open'},
            {'a': '1', 'state': 'close'},
        ]
        for record in records:
            try:
                rec = aggregateplugin.process(record)
                aggregateplugin.core.db.write('record', rec)
            except AbortAndUpdate as e:
                aggregateplugin.core.db.write('record', e.record or record)
                continue
        results = aggregateplugin.core.db.search('record', ['=', 'aggregate', 'Agg1'])['data'][0]
        assert results['duplicates'] == 2
        assert results['state'] == 'close'

    def test_aggregate_flapping(self, aggregateplugin):
        records = [
            {'a': '4', 'c': '1', 'state': 'open'},
            {'a': '4', 'c': '1', 'state': 'close'},
            {'a': '4', 'c': '2', 'state': 'open'},
            {'a': '4', 'c': '3', 'state': 'open'},
            {'a': '4', 'c': '4', 'state': 'open'},
        ]
        for record in records:
            try:
                rec = aggregateplugin.process(record)
                aggregateplugin.core.db.write('record', rec)
            except AbortAndUpdate as e:
                aggregateplugin.core.db.write('record', e.record or record)
                continue
        results = aggregateplugin.core.db.search('record', ['=', 'aggregate', 'Agg4'])['data'][0]
        assert results['duplicates'] == 5
        assert results['flapping_countdown'] == -1
