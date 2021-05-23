#!/usr/bin/python3.6

from snooze.utils import Action

from logging import getLogger
log = getLogger('snooze.tests.action')

def test_action_set():
    record = {'a': 1, 'b': 2}
    action = Action('SET', 'c', 3)
    return_code = action.modify(record)
    assert record == {'a': 1, 'b': 2, 'c': 3}
    assert return_code

def test_action_delete():
    record = {'a': 1, 'b': 2}
    action = Action('DELETE', 'b')
    return_code = action.modify(record)
    assert record == {'a': 1}
    assert return_code

def test_action_array_append():
    record = {'a': 1, 'b': ['1', '2', '3']}
    action = Action('ARRAY_APPEND', 'b', '4')
    return_code = action.modify(record)
    assert record == {'a': 1, 'b': ['1', '2', '3', '4']}
    assert return_code

def test_action_array_delete():
    record = {'a': 1, 'b': ['1', '2', '3']}
    action = Action('ARRAY_DELETE', 'b', '2')
    return_code = action.modify(record)
    assert record == {'a': 1, 'b': ['1', '3']}

def test_action_template():
    record = {'a': '1', 'b': '2'}
    action = Action('SET_TEMPLATE', 'c', '{{ (a | int) + (b | int) }}')
    log.debug("Record before: {}".format(record))
    return_code = action.modify(record)
    log.debug("Record after: {}".format(record))
    assert record == {'a': '1', 'b': '2', 'c': '3'}
