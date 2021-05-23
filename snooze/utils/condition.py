#!/usr/bin/python3.6

from .functions import dig, flatten

import re
import logging
from logging import getLogger
LOG = getLogger('snooze.condition')

class Condition():
    def __init__(self, array=None):
        LOG.debug("Creating condition with {}".format(array))
        self.array = array
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
            LOG.debug("No condition, auto match the rule")
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
            LOG.debug("Rule: {}, Record: {}".format(value, record_value))
            return record_value and (record_value == value)
        elif operation == '!=':
            key, value = args
            record_value = dig(record, *key.split('.'))
            LOG.debug("Rule: {}, Record: {}".format(value, record_value))
            return record_value and (record_value != value)
        elif operation == '>':
            key, value = args
            record_value = dig(record, *key.split('.'))
            try:
                newval = float(value)
                newrecval = float(record_value)
            except ValueError:
                newval = value
                newrecval = record_value
            LOG.debug("Rule: {}, Record: {}".format(newval, newrecval))
            return record_value and (newrecval > newval)
        elif operation == '<':
            key, value = args
            record_value = dig(record, *key.split('.'))
            try:
                newval = float(value)
                newrecval = float(record_value)
            except ValueError:
                newval = value
                newrecval = record_value
            LOG.debug("Rule: {}, Record: {}".format(newval, newrecval))
            return record_value and (newrecval < newval)
        elif operation == 'MATCHES':
            key, value = args
            record_value = dig(record, *key.split('.'))
            LOG.debug("Rule: {}, Record: {}".format(value, record_value))
            if record_value:
                if len(value) > 1 and value[0] == '/' and value[-1] == '/':
                    value = value[1:-1]
                reg = re.compile(value, flags=re.IGNORECASE)
                return reg.search(record_value) is not None
            else:
                return False
        elif operation == 'EXISTS':
            record_value = dig(record, *args[0].split('.'))
            LOG.debug("Rule: {} exists, Record: {}".format(args[0], record_value))
            return record_value is not None
        elif operation == 'CONTAINS':
            key, value = args
            record_value = dig(record, *key.split('.'))
            LOG.debug("Rule: {}, Record: {}".format(value, record_value))
            return isinstance(record_value, list) and any(value.casefold() in a.casefold() for a in flatten(record_value))
