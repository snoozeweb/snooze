#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

#!/usr/bin/python3.6

import re
from abc import abstractmethod
from logging import getLogger

from jinja2 import Template

log = getLogger('snooze.utils.modification')

class ModificationException(Exception): pass

def resolve(record, args):
    '''Return the arguments evaluated if it's a template'''
    return [
        Template(arg).render(record)
        if isinstance(arg, str) else arg
        for arg in args
    ]

class Modification:
    '''A class to represent a modification'''

    def __init__(self, *args):
        self.args = args

    @abstractmethod
    def modify(self, record):
        pass

class SetOperation(Modification):
    def modify(self, record):
        key, value = resolve(record, self.args)
        return_code = bool(value and record.get(key) != value)
        record[key] = value
        return return_code

class DeleteOperation(Modification):
    def modify(self, record):
        key, *_ = resolve(record, self.args)
        try:
            del record[key]
            return True
        except KeyError:
            return False

class ArrayAppendOperation(Modification):
    def modify(self, record):
        key, value = resolve(record, self.args)
        array = record.get(key)
        if isinstance(array, list):
            record[key] += value
            return True
        return False

class ArrayDeleteOperation(Modification):
    def modify(self, record):
        key, value = resolve(record, self.args)
        try:
            record[key].remove(value)
            return True
        except (ValueError, KeyError):
            return False

class RegexParse(Modification):
    def modify(self, record):
        try:
            key, regex = resolve(record, self.args)
            results = re.search(regex, record[key])
            if results:
                for name, value in results.groupdict({}).items():
                    record[name] = value
                return True
            return False
        except KeyError:
            return False
        except re.error as err:
            log.warning("Syntax error in REGEX_PARSE: regex `%s` has error: %s", regex, err)
            return False

OPERATIONS = {
    'SET': SetOperation,
    'DELETE': DeleteOperation,
    'ARRAY_APPEND': ArrayAppendOperation,
    'ARRAY_DELETE': ArrayDeleteOperation,
    'REGEX_PARSE': RegexParse,
}

def get_modification(operation, *args):
    '''Return the modification class to run'''
    return OPERATIONS[operation](*args)
