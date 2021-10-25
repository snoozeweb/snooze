#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

#!/usr/bin/python3.6

from snooze.utils import Condition
def test_match_equal_1():
    record = {'a': '1', 'b': '2'}
    search = ['=', 'a', '1']
    assert Condition(search).match(record)

def test_match_equal_2():
    record = {'a': '1', 'b': {'c': 1}}
    search = ['=', 'b.c', 1]
    assert Condition(search).match(record)

def test_match_equal_3():
    record = {'a': '1', 'b': {'c': 1}}
    search = ['=', 'a.c', '2']
    assert not Condition(search).match(record)

def test_match_equal_4():
    record = {'a': [1, 2]}
    search = ['=', 'a.1', 2]
    assert Condition(search).match(record)

def test_match_different_1():
    record = {'a': '1', 'b': '2'}
    search = ['!=', 'a', '1']
    assert not Condition(search).match(record)

def test_match_greater_1():
    record = {'a': 1, 'b': 2}
    search = ['>', 'b', '1']
    assert Condition(search).match(record)

def test_match_lower_1():
    record = {'var': 'aa'}
    search = ['<', 'var', 'ab']
    assert Condition(search).match(record)

def test_match_and():
    record = {'a': 1, 'b': 2}
    search = ['AND', ['=', 'a', 1], ['=', 'b', 2]]
    assert Condition(search).match(record)

def test_match_or():
    record = {'a': 1, 'b': 3}
    search = ['OR', ['=', 'a', 1], ['=', 'b', 2]]
    assert Condition(search).match(record)

def test_match_not():
    record = {'a': 1, 'b': 3}
    search = ['NOT', ['=', 'a', 1]]
    assert not Condition(search).match(record)

def test_match_regex():
    record = {'a': '__pattern__'}
    search = ['MATCHES', 'a', '/pattern/']
    assert Condition(search).match(record)

def test_match_exists():
    record = {'a': '1'}
    search = ['EXISTS', 'b']
    assert not Condition(search).match(record)

def test_match_contains():
    record1 = {'a': ['0', ['11', '2'], '3']}
    search1 = ['CONTAINS', 'a', '1']
    assert Condition(search1).match(record1)
    record2 = {'a': '11'}
    search2 = ['CONTAINS', 'a', ['0', '1']]
    assert Condition(search2).match(record2)

def test_match_in():
    record1 = {'a': ['0', ['11', '2'], '3']}
    search1 = ['IN', ['1', '5'], 'a']
    assert not Condition(search1).match(record1)
    record2 = {'a': '1'}
    search2 = ['IN', ['1', '5'], 'a']
    assert Condition(search2).match(record2)

def test_match_in_condition():
    record1 = {'a': [{'b':'0'}, {'c': '0'}]}
    search1 = ['IN', ['=', 'c', '0'], 'a']
    assert Condition(search1).match(record1)
    record2 = {'a': [{'b':'0'}, {'c': '0'}]}
    search2 = ['IN', ['=', 'd', '0'], 'a']
    assert not Condition(search2).match(record2)

def test_match_search():
    record = {'myfield': [{'b':'mystring'}, {'mysearch': '0'}]}
    search1 = ['SEARCH', 'field']
    assert Condition(search1).match(record)
    search2 = ['SEARCH', 'string']
    assert Condition(search2).match(record)
    search3 = ['SEARCH', 'search']
    assert Condition(search3).match(record)
    search4 = ['SEARCH', 'value']
    assert not Condition(search4).match(record)

def test_str():
    search = ['OR', ['NOT', ['=', 'a', 1]], ['=', 'b', 2]]
    assert str(Condition(search)) == "((!(a = 1)) OR (b = 2))"
