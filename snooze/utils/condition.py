#!/usr/bin/python3.6

from .functions import dig, flatten

import re
import logging
from logging import getLogger
LOG = getLogger('snooze.condition')

class OperationNotSupported(Exception): pass

class Condition():
    def __init__(self, array=None):
        LOG.debug("Creating condition with {}".format(array))
        self.array = array
        if type(self.array) is list and len(self.array) > 0 and self.array[0] is None:
            LOG.debug("Condition None will always match")
            self.array = None
    def __str__(self):
        if not self.array:
            return "True"
        operation, *args = self.array
        conds = list(map(Condition, args))
        if operation in ['NOT']:
            return "(!{})".format(str(conds[0]))
        elif operation in ['AND', 'OR']:
            return "({} {} {})".format(str(conds[0]), operation, str(conds[1]))
        else:
            arg1, arg2 = args
            return "({} {} {})".format(arg1, operation, arg2)
    def match(self, record):
        """
        Input: Dict
        Output: Boolean
        """
        if not self.array:
            LOG.debug("No condition, auto match the condition")
            return True
        operation, *args = self.array
        LOG.debug("Operation: {}, Args: {}".format(operation, args))
        if operation == 'AND':
            cond1, cond2 = map(Condition, args)
            return (cond1.match(record) and cond2.match(record))
        elif operation == 'OR':
            cond1, cond2 = map(Condition, args)
            return (cond1.match(record) or cond2.match(record))
        elif operation == 'NOT':
            cond = Condition(args[0])
            return (not cond.match(record))
        elif operation == '=':
            key, value = args
            record_value = dig(record, *key.split('.'))
            LOG.debug("Value: {}, Record: {}".format(value, record_value))
            return record_value is not None and (record_value == value)
        elif operation == '!=':
            key, value = args
            record_value = dig(record, *key.split('.'))
            LOG.debug("Value: {}, Record: {}".format(value, record_value))
            return record_value != value
        elif operation == '>':
            key, value = args
            record_value = dig(record, *key.split('.'))
            try:
                newval = float(value)
                newrecval = float(record_value)
            except ValueError:
                newval = value
                newrecval = record_value
            LOG.debug("Value: {}, Record: {}".format(newval, newrecval))
            return record_value is not None and (newrecval > newval)
        elif operation == '>=':
            key, value = args
            record_value = dig(record, *key.split('.'))
            try:
                newval = float(value)
                newrecval = float(record_value)
            except ValueError:
                newval = value
                newrecval = record_value
            LOG.debug("Value: {}, Record: {}".format(newval, newrecval))
            return record_value is not None and (newrecval >= newval)
        elif operation == '<':
            key, value = args
            record_value = dig(record, *key.split('.'))
            try:
                newval = float(value)
                newrecval = float(record_value)
            except ValueError:
                newval = value
                newrecval = record_value
            LOG.debug("Value: {}, Record: {}".format(newval, newrecval))
            return record_value is not None and (newrecval < newval)
        elif operation == '<=':
            key, value = args
            record_value = dig(record, *key.split('.'))
            try:
                newval = float(value)
                newrecval = float(record_value)
            except ValueError:
                newval = value
                newrecval = record_value
            LOG.debug("Value: {}, Record: {}".format(newval, newrecval))
            return record_value is not None and (newrecval <= newval)
        elif operation == 'MATCHES':
            key, value = args
            record_value = dig(record, *key.split('.'))
            LOG.debug("Value: {}, Record: {}".format(value, record_value))
            if record_value:
                if len(value) > 1 and value[0] == '/' and value[-1] == '/':
                    value = value[1:-1]
                reg = re.compile(value, flags=re.IGNORECASE)
                return reg.search(record_value) is not None
            else:
                return False
        elif operation == 'EXISTS':
            record_value = dig(record, *args[0].split('.'))
            LOG.debug("Value: {} exists, Record: {}".format(args[0], record_value))
            return record_value is not None
        elif operation == 'CONTAINS':
            key, value = args
            record_value = dig(record, *key.split('.'))
            if not isinstance(value, list):
                value = [value]
            if not isinstance(record_value, list):
                record_value = [record_value]
            LOG.debug("Value: {}, Record: {}".format(value, record_value))
            for val in flatten(value):
                reg = re.compile(val, flags=re.IGNORECASE)
                for record in flatten(record_value): 
                    if reg.search(record):
                        return True
            return False
        elif operation == 'IN':
            key, value = args
            record_value = dig(record, *value.split('.'))
            if not isinstance(record_value, list):
                record_value = [record_value]
            if not isinstance(key, list):
                key = [key]
            else:
                try:
                    saved_key = key
                    key = Condition(key)
                    return any(map(key.match, record_value))
                except:
                    key = saved_key
                    LOG.debug("{} is not a Condition, using default match".format(key))
            LOG.debug("Value: {}, Record: {}".format(key, record_value))
            return any(a in flatten(key) for a in flatten(record_value))
        elif operation == 'SEARCH':
            cond = args[0]
            LOG.debug("Search value '{}' in record: {}".format(cond, record))
            return cond in str(record)
        else:
            raise OperationNotSupported(operation)
