#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''Module for managing modification objects.
Modifications are used by the rule core plugin to modify records
automatically based on a rule.
'''

import re
from abc import abstractmethod
from logging import getLogger
from typing import Optional

from jinja2 import Template

from snooze.utils.typing import Record

log = getLogger('snooze.utils.modification')

class OperationNotSupported(Exception):
    '''Exception raised when the modification type doesn't exist'''
    def __init__(self, name):
        message = f"Modification type '{name}' is not supported"
        super().__init__(message)

class ModificationInvalid(RuntimeError):
    '''Exception raise when there was an error when creating a modification'''
    def __init__(self, name, args, err):
        message = f"Error in modification '{name}' ({args}): {err}"
        super().__init__(message)

def resolve(record: Record, args: list):
    '''Return the arguments evaluated if it's a template'''
    return [
        Template(arg).render(record)
        if isinstance(arg, str) else arg
        for arg in args
    ]

class Modification:
    '''A class to represent a modification'''

    def __init__(self, args: list, core: 'Optional[Core]' = None):
        self.core = core
        self.args = args

    @abstractmethod
    def modify(self, record: Record) -> bool:
        '''Called when the modification should be applied to a record'''

    def pprint(self) -> str:
        '''Pretty print of the modification object'''
        return f"{self.__class__.__name__}({self.args})"

class SetOperation(Modification):
    '''Set a key to a given value'''
    def modify(self, record: Record) -> bool:
        key, value = resolve(record, self.args)
        try:
            return_code = bool(value and record.get(key) != value)
            record[key] = value
            return return_code
        except Exception:
            return False

class DeleteOperation(Modification):
    '''Delete a given key'''
    def modify(self, record: Record) -> bool:
        key, *_ = resolve(record, self.args)
        try:
            del record[key]
            return True
        except KeyError:
            return False

class ArrayAppendOperation(Modification):
    '''Append an element to a key, if this key is an array/list'''
    def modify(self, record: Record) -> bool:
        key, value = resolve(record, self.args)
        array = record.get(key)
        if isinstance(array, list):
            record[key] += value
            return True
        return False

class ArrayDeleteOperation(Modification):
    '''Delete an element from an array/list, by value'''
    def modify(self, record: Record) -> bool:
        key, value = resolve(record, self.args)
        try:
            record[key].remove(value)
            return True
        except (ValueError, KeyError):
            return False

class RegexParse(Modification):
    '''Given a key and a regex with named capture groups, parse the
    key's value, and merge the captured elements with the record'''
    def modify(self, record: Record) -> bool:
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

class RegexSub(Modification):
    '''Apply a regex search and replace expression to a key's value'''
    def modify(self, record: Record) -> bool:
        key, out_key, regex, sub = resolve(record, self.args)
        try:
            value = record[key]
            new_value = re.sub(regex, sub, value)
            record[out_key] = new_value
            return True
        except (KeyError, TypeError):
            return False
        except re.error as err:
            log.warning("Syntax error in REGEX_SUB: regex `%s` has error: %s", regex, err)
            return False

class KvSet(Modification):
    '''Match the key's value with the corresponding value from the kv core plugin'''
    def __init__(self, args: list, core: 'Optional[Core]'):
        super().__init__(args, core)
        self.dict, self.key, self.out_field = args
        self.kv_plugin = core.get_core_plugin('kv')
        if not self.kv_plugin:
            raise Exception('KV plugin not present. Could not load Modification')
    def modify(self, record: Record) -> bool:
        try:
            record_key = record[self.key]
            log.debug("Check if Record has key: %s=%s", self.key, record_key)
            out_value = self.kv_plugin.get(self.dict, record_key)
            log.debug("Found key-value: %s[%s] = %s", self.dict, record_key, out_value)
            record[self.out_field] = out_value
            return True
        except (KeyError, IndexError):
            return False


OPERATIONS = {
    'SET': SetOperation,
    'DELETE': DeleteOperation,
    'ARRAY_APPEND': ArrayAppendOperation,
    'ARRAY_DELETE': ArrayDeleteOperation,
    'REGEX_PARSE': RegexParse,
    'REGEX_SUB': RegexSub,
    'KV_SET': KvSet,
}

def validate_modification(obj: list, core: 'Optional[Core]' = None):
    '''Raise an exception if the object contains an invalid modification'''
    modifications = obj.get('modifications', [])
    for modification in modifications:
        get_modification(modification, core=core)

def get_modification(args: list, core: 'Optional[Core]' = None) -> Modification:
    '''Return the modification class to run'''
    try:
        operation = args[0]
        modification = args[1:]
        return OPERATIONS[operation](modification, core=core)
    except IndexError as err:
        raise Exception(f"Error with modification `{args}`") from err
    except KeyError as err:
        raise OperationNotSupported(operation) from err
    except TypeError as err:
        raise ModificationInvalid(operation, args, err) from err
