#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#
'''
Tests for conditions. Each condition is tested in its own class.
'''

import pytest
from snooze.utils.condition import *

class TestEquals:
    def test_get_condition(self):
        condition = get_condition(['=', 'a', '0'])
        assert isinstance(condition, Equals)
    def test_init(self):
        condition = Equals(['=', 'a', '0'])
        assert isinstance(condition, Condition)
    def test_match_simple(self):
        record = {'a': '1', 'b': '2'}
        condition = Equals(['=', 'a', '1'])
        assert condition.match(record) == True
    def test_match_nested_dict(self):
        record = {'a': '1', 'b': {'c': '1'}}
        condition = Equals(['=', 'b.c', '1'])
        assert condition.match(record) == True
    def test_miss_nested_dict(self):
        record = {'a': '1', 'b': {'c': 1}}
        condition = Equals(['=', 'a.c', '2'])
        assert condition.match(record) == False
    def test_match_nested_list(self):
        record = {'a': ['1', '2']}
        condition = Equals(['=', 'a.1', '2'])
        assert condition.match(record) == True
    def test_str(self):
        condition = Equals(['=', 'a.1', '2'])
        assert str(condition) == "(a.1 = '2')"
    def test_edge_no_field(self):
        record = {'a': '1'}
        with pytest.raises(ConditionInvalid):
            condition = Equals(['=', None, '1'])
    def test_edge_no_value(self):
        record = {'a': '1'}
        condition = Equals(['=', 'a', None])
        assert condition.match(record) == False

class TestNotEquals:
    def test_get_condition(self):
        condition = get_condition(['!=', 'a', '0'])
        assert isinstance(condition, NotEquals)
    def test_init(self):
        condition = NotEquals(['!=', 'a', '1'])
        assert isinstance(condition, Condition)
    def test_miss(self):
        record = {'a': '1', 'b': '2'}
        condition = NotEquals(['!=', 'a', '1'])
        assert condition.match(record) == False
    def test_str(self):
        condition = NotEquals(['!=', 'a', 1])
        assert str(condition) == "(a != 1)"

class TestGreaterThan:
    def test_get_condition(self):
        condition = get_condition(['>', 'a', '0'])
        assert isinstance(condition, GreaterThan)
    def test_init(self):
        condition = GreaterThan(['>', 'b', '1'])
        assert isinstance(condition, Condition)
    def test_match_two_float(self):
        record = {'a': 1.0, 'b': 2.0}
        condition = GreaterThan(['>', 'b', 1.0])
        assert condition.match(record) == True
    def test_match_string_and_integer(self):
        record = {'a': 1, 'b': 2}
        condition = GreaterThan(['>', 'b', '1'])
        assert condition.match(record) == True
    def test_str(self):
        condition = GreaterThan(['>', 'x', 100])
        assert str(condition) == "(x > 100)"

class TestLowerThan:
    def test_get_condition(self):
        condition = get_condition(['<', 'a', '0'])
        assert isinstance(condition, LowerThan)
    def test_init(self):
        condition = LowerThan(['<', 'var', 'ab'])
        assert isinstance(condition, Condition)
    def test_match_two_string(self):
        record = {'var': 'aa'}
        condition = LowerThan(['<', 'var', 'ab'])
        assert condition.match(record) == True
    def test_str(self):
        condition = LowerThan(['<', 'x', 100])
        assert str(condition) == "(x < 100)"

class TestAnd:
    def test_get_condition(self):
        condition = get_condition(['AND', ['=', 'a', 1], ['=', 'b', 2]])
        assert isinstance(condition, And)
    def test_init(self):
        condition = And(['AND', ['=', 'a', 1], ['=', 'b', 2]])
        assert isinstance(condition, Condition)
    def test_and_operation(self):
        condition = Equals(['=', 'a', 1]) & Equals(['=', 'b', 2])
        assert isinstance(condition, And)
    def test_match(self):
        record = {'a': 1, 'b': 2}
        condition = And(['AND', ['=', 'a', 1], ['=', 'b', 2]])
        assert condition.match(record) == True
    def test_str(self):
        condition = Equals(['=', 'a', 1]) & Equals(['=', 'b', 2])
        assert str(condition) == "((a = 1) & (b = 2))"

class TestOr:
    def test_get_condition(self):
        condition = get_condition(['OR', ['=', 'a', 1], ['=', 'b', 2]])
        assert isinstance(condition, Or)
    def test_init(self):
        condition = Or(['OR', ['=', 'a', 1], ['=', 'b', 2]])
        assert isinstance(condition, Condition)
    def test_match(self):
        record = {'a': 1, 'b': 3}
        condition = Or(['OR', ['=', 'a', 1], ['=', 'b', 2]])
        assert condition.match(record) == True
    def test_str(self):
        condition = Equals(['=', 'a', 1]) | Equals(['=', 'b', 2])
        assert str(condition) == "((a = 1) | (b = 2))"

class TestNot:
    def test_get_condition(self):
        condition = get_condition(['NOT', ['=', 'a', 1]])
        assert isinstance(condition, Not)
    def test_init(self):
        condition = Not(['NOT', ['=', 'a', 1]])
        assert isinstance(condition, Condition)
    def test_match(self):
        record = {'a': 1, 'b': 3}
        condition = Not(['NOT', ['=', 'a', 2]])
        assert condition.match(record) == True
    def test_miss(self):
        record = {'a': 1, 'b': 3}
        condition = Not(['NOT', ['=', 'a', 1]])
        assert condition.match(record) == False
    def test_str(self):
        condition = ~Equals(['=', 'b', 2])
        assert str(condition) == "!(b = 2)"

class TestMatches:
    def test_get_condition(self):
        condition = get_condition(['MATCHES', 'a', 'string'])
        assert isinstance(condition, Matches)
    def test_match(self):
        record = {'a': '__pattern__'}
        condition = Matches(['MATCHES', 'a', 'pattern'])
        assert condition.match(record) == True
    def test_match_sugar(self):
        record = {'a': '__pattern__'}
        condition = Matches(['MATCHES', 'a', '/pattern/'])
        assert condition.match(record) == True
    def test_str(self):
        condition = Matches(['MATCHES', 'a', '/string/'])
        assert str(condition) == "(a ~ '/string/')"

class TestExists:
    def test_get_condition(self):
        condition = get_condition(['EXISTS', 'a'])
        assert isinstance(condition, Exists)
    def test_match(self):
        record = {'a': '1'}
        condition = Exists(['EXISTS', 'a'])
        assert condition.match(record) == True
    def test_miss(self):
        record = {'a': '1'}
        condition = Exists(['EXISTS', 'b'])
        assert condition.match(record) == False
    def test_str(self):
        condition = Exists(['EXISTS', 'a'])
        assert str(condition) == "a?"

class TestContains:
    def test_get_condition(self):
        condition = get_condition(['CONTAINS', 'a', 'substring'])
        assert isinstance(condition, Contains)
    def test_match_search_in_string(self):
        record = {'a': ['0', ['11', '2'], '3']}
        condition = Contains(['CONTAINS', 'a', '1'])
        assert condition.match(record) == True
    def test_match_incomplete_list(self):
        record = {'a': '11'}
        condition = Contains(['CONTAINS', 'a', ['0', '1']])
        assert condition.match(record) == True
    def test_str(self):
        condition = Contains(['CONTAINS', 'a', 'substring'])
        assert str(condition) == "(a contains 'substring')"

class TestIn:
    def test_get_condition(self):
        condition = get_condition(['IN', ['1', '2', '3'], 'a'])
        assert isinstance(condition, In)
    def test_match_list(self):
        record = {'a': '1'}
        condition = In(['IN', ['1', '5'], 'a'])
        assert condition.match(record) == True
    def test_miss_list(self):
        record = {'a': ['0', ['11', '2'], '3']}
        condition = In(['IN', ['1', '5'], 'a'])
        assert condition.match(record) == False
    def test_match_condition(self):
        record = {'a': [{'b':'0'}, {'c': '0'}]}
        condition = In(['IN', ['=', 'c', '0'], 'a'])
        assert condition.match(record) == True
    def test_miss_condition(self):
        record = {'a': [{'b':'0'}, {'c': '0'}]}
        condition = In(['IN', ['=', 'd', '0'], 'a'])
        assert condition.match(record) == False
    def test_str(self):
        condition = In(['IN', 'element', 'a'])
        assert str(condition) == "('element' in a)"

class TestSearch:
    def test_get_condition(self):
        condition = get_condition(['SEARCH', 'string'])
        assert isinstance(condition, Search)
    def test_match_incomplete_field(self):
        record = {'myfield': [{'b':'mystring'}, {'mysearch': '0'}]}
        condition = Search(['SEARCH', 'field'])
        assert condition.match(record) == True
    def test_match_nested_value(self):
        record = {'myfield': [{'b':'mystring'}, {'mysearch': '0'}]}
        condition = Search(['SEARCH', 'string'])
        assert condition.match(record) == True
    def test_match_incomplete_nested_field(self):
        record = {'myfield': [{'b':'mystring'}, {'mysearch': '0'}]}
        condition = Search(['SEARCH', 'search'])
        assert condition.match(record) == True
    def test_miss(self):
        record = {'myfield': [{'b':'mystring'}, {'mysearch': '0'}]}
        condition = Search(['SEARCH', 'value'])
        assert condition.match(record) == False
    def test_str(self):
        condition = Search(['SEARCH', 'mystring'])
        assert str(condition) == "(SEARCH 'mystring')"

class TestAlwaysTrue:
    def test_get_condition(self):
        conditions = [
            [],
            [''],
            [None],
        ]
        for condition_str in conditions:
            condition = get_condition(condition_str)
            assert isinstance(condition, AlwaysTrue)

    def test_match(self):
        records = [
            {'a': 1},
            {'b': '2'},
            {},
        ]
        condition = AlwaysTrue()
        for record in records:
            assert condition.match(record) == True
