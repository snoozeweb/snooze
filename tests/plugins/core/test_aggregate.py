#!/usr/bin/python3.6

from snooze.plugins.core.aggregaterule.plugin import Aggregaterule, AggregateruleObject

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
    def aggregateplugin(self, core, config):
        aggregate_rules = [
            {'name': 'Agg1', 'condition': ['=', 'a', '1'], 'fields': ['a', 'b']},
            {'name': 'Agg2', 'condition': ['=', 'a', '2'], 'fields': ['a', 'c']}
        ]
        core.db.write('aggregaterule', aggregate_rules)
        return Aggregaterule(core, config)
    def test_process(self, aggregateplugin):
        records = [
            # Agg1 - 1
            {'a': '1', 'b': '2', 'c': '3'},
            {'a': '1', 'b': '2', 'c': '3'},
            {'a': '1', 'b': '2', 'c': '4'},
            # Agg1 - 2
            {'a': '1', 'b': '0', 'c': '0'},
            # Agg2 - 1
            {'a': '2', 'b': '1', 'c': '4'},
            {'a': '2', 'b': '2', 'c': '4'},
            # Agg2 - 2
            {'a': '2', 'b': '1', 'c': '3'},
            # Default
            {'a': '3', 'b': '1', 'c': '3'},
        ]
        for record in records:
            aggregateplugin.process(record)
        results1 = aggregateplugin.core.db.search('aggregate', ['=', 'aggregate', 'Agg1'])['data']
        results2 = aggregateplugin.core.db.search('aggregate', ['=', 'aggregate', 'Agg2'])['data']
        results3 = aggregateplugin.core.db.search('aggregate', ['=', 'aggregate', 'default'])['data']
        assert results1[0]['duplicates'] == 3 and results1[1]['duplicates'] == 1 and results2[0]['duplicates'] == 2 and results2[1]['duplicates'] == 1 and results3[0]['duplicates'] == 1
