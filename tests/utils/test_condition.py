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
    record = {'a': ['0', ['11', '2'], '3']}
    search = ['CONTAINS', 'a', '1']
    assert Condition(search).match(record)

def test_match_in():
    record = {'a': ['0', ['11', '2'], '3']}
    search = ['IN', '1', 'a']
    assert not Condition(search).match(record)

def test_str():
    search = ['OR', ['NOT', ['=', 'a', 1]], ['=', 'b', 2]]
    assert str(Condition(search)) == "((!(a = 1)) OR (b = 2))"
