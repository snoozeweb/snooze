#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

#!/usr/bin/python3.6

from snooze.utils import get_modification

from logging import getLogger
log = getLogger('snooze.tests.modification')

def test_modification_set():
    record = {'a': 1, 'b': 2}
    modification = get_modification('SET', 'c', 3)
    return_code = modification.modify(record)
    assert record == {'a': 1, 'b': 2, 'c': 3}
    assert return_code

def test_modification_delete():
    record = {'a': 1, 'b': 2}
    modification = get_modification('DELETE', 'b')
    return_code = modification.modify(record)
    assert record == {'a': 1}
    assert return_code

def test_modification_array_append():
    record = {'a': 1, 'b': ['1', '2', '3']}
    modification = get_modification('ARRAY_APPEND', 'b', '4')
    return_code = modification.modify(record)
    assert record == {'a': 1, 'b': ['1', '2', '3', '4']}
    assert return_code

def test_modification_array_delete():
    record = {'a': 1, 'b': ['1', '2', '3']}
    modification = get_modification('ARRAY_DELETE', 'b', '2')
    return_code = modification.modify(record)
    assert record == {'a': 1, 'b': ['1', '3']}

def test_modification_template():
    record = {'a': '1', 'b': '2'}
    modification = get_modification('SET', 'c', '{{ (a | int) + (b | int) }}')
    log.debug("Record before: {}".format(record))
    return_code = modification.modify(record)
    log.debug("Record after: {}".format(record))
    assert record == {'a': '1', 'b': '2', 'c': '3'}

def test_modification_regex_parse():
    record = {'message': 'CRON[12345]: Error during cronjob'}
    modification = get_modification('REGEX_PARSE', 'message', '(?P<appname>.*?)\[(?P<pid>\d+)\]: (?P<message>.*)')
    return_code = modification.modify(record)
    assert return_code
    assert record == {'message': 'Error during cronjob', 'appname': 'CRON', 'pid': '12345'}

def test_modification_regex_parse_broken_regex():
    record = {'message': 'CRON[12345]: Error during cronjob'}
    modification = get_modification('REGEX_PARSE', 'message', '(?P<appname.*?)\[(?P<pid>\d+)\]: (?P<message>.*)')
    return_code = modification.modify(record)
    assert return_code == False
    assert record == {'message': 'CRON[12345]: Error during cronjob'}

def test_modification_regex_sub():
    record = {'message': 'Error in session 0x2134adf890bc89'}
    modification = get_modification('REGEX_SUB', 'message', 'message', '0x[a-fA-F0-9]+', '0x###')
    return_code = modification.modify(record)
    assert return_code == True
    assert record == {'message': 'Error in session 0x###'}
