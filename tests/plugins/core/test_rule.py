#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

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
        rule = {'name': 'Rule1', 'condition': ['=', 'a', '99999'], 'modifications': [ ['SET', 'a', '2'], ['SET', 'c', '3'] ]}
        RuleObject(rule).modify(record)
        assert record == {'a': '2', 'b': '2', 'c': '3'}

class TestRulesPlugin:
    @pytest.fixture
    def ruleplugin(self, core):
        rules = [
            {'name': 'Rule1', 'condition': ['=', 'a', '1'], 'modifications': [['SET', 'c', '1']]}
        ]
        uid = core.db.write('rule', rules)['data']['added'][0]
        children_rules = [
            {'name': 'SubRule1', 'condition': ['=', 'c', '1'], 'modifications': [ ['SET', 'c', '4'], ['SET', 'b', '4'] ], 'parent': uid}
        ]
        uid = core.db.write('rule', children_rules)['data']['added'][0]
        children_rules = [
            {'name': 'SubSubRule1', 'condition': ['=', 'c', '4'], 'modifications': [ ['SET', 'c', '5'] ], 'parent': uid}
        ]
        core.db.write('rule', children_rules)
        rule = Rule(core)
        rule.post_init()
        return rule
    def test_process(self, ruleplugin):
        record = {'a': '1', 'b': '2'}
        ruleplugin.process(record)
        assert record == {'a': '1', 'b': '4', 'c': '5', 'rules': ['Rule1', 'SubRule1', 'SubSubRule1']}
