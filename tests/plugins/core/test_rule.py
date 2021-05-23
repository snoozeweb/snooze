#!/usr/bin/python3.6

from snooze.plugins.core.rule.plugin import Rule, RuleObject

import pytest

class TestRule:
    @pytest.fixture
    def record(self):
        return {'a': '1', 'b': '2'}
    def test_match(self, record):
        rule = {'name': 'Rule1', 'condition': ['=', 'a', '1']}
        RuleObject(rule).match(record)
        assert record == {'a': '1', 'b': '2', 'rules': ['Rule1']}
    def test_modify(self, record):
        rule = {'name': 'Rule1', 'condition': ['=', 'a', '99999'], 'actions': [ ['SET', 'a', '2'], ['SET', 'c', '3'] ]}
        RuleObject(rule).modify(record)
        assert record == {'a': '2', 'b': '2', 'c': '3'}
    def test_process(self, record):
        rule = {'name': 'Rule1', 'condition': ['=', 'a', '1'], 'actions': [ ['SET', 'a', '2'], ['SET', 'c', '3'] ]}
        RuleObject(rule).process(record)
        assert record == {'a': '2', 'b': '2', 'c': '3', 'rules': ['Rule1']}

class TestRulesPlugin:
    @pytest.fixture
    def ruleplugin(self, core, config):
        rules = [
            {'name': 'Rule1', 'condition': ['=', 'a', '1'], 'actions': [['SET', 'c', '1']]}
        ]
        uid = core.db.write('rule', rules)['data']['added'][0]
        children_rules = [
            {'name': 'SubRule1', 'condition': ['=', 'c', '1'], 'actions': [ ['SET', 'c', '4'], ['SET', 'b', '4'] ], 'parent': uid}
        ]
        core.db.write('rule', children_rules)
        return Rule(core, config)
    def test_process(self, ruleplugin):
        record = {'a': '1', 'b': '2'}
        ruleplugin.process(record)
        assert record == {'a': '1', 'b': '4', 'c': '4', 'rules': ['Rule1', 'SubRule1']}
