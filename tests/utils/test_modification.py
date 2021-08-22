#!/usr/bin/python3.6

from snooze.utils import Modification

from logging import getLogger
log = getLogger('snooze.tests.modification')

def test_modification_set():
    record = {'a': 1, 'b': 2}
    modification = Modification('SET', 'c', 3)
    return_code = modification.modify(record)
    assert record == {'a': 1, 'b': 2, 'c': 3}
    assert return_code

def test_modification_delete():
    record = {'a': 1, 'b': 2}
    modification = Modification('DELETE', 'b')
    return_code = modification.modify(record)
    assert record == {'a': 1}
    assert return_code

def test_modification_array_append():
    record = {'a': 1, 'b': ['1', '2', '3']}
    modification = Modification('ARRAY_APPEND', 'b', '4')
    return_code = modification.modify(record)
    assert record == {'a': 1, 'b': ['1', '2', '3', '4']}
    assert return_code

def test_modification_array_delete():
    record = {'a': 1, 'b': ['1', '2', '3']}
    modification = Modification('ARRAY_DELETE', 'b', '2')
    return_code = modification.modify(record)
    assert record == {'a': 1, 'b': ['1', '3']}

def test_modification_template():
    record = {'a': '1', 'b': '2'}
    modification = Modification('SET', 'c', '{{ (a | int) + (b | int) }}')
    log.debug("Record before: {}".format(record))
    return_code = modification.modify(record)
    log.debug("Record after: {}".format(record))
    assert record == {'a': '1', 'b': '2', 'c': '3'}
