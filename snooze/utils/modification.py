#!/usr/bin/python3.6

class ModificationException(Exception): pass

from jinja2 import Template

from logging import getLogger
log = getLogger('snooze.utils.modification')

class Modification():
    def __init__(self, operation, key, value=None):
        self.operation = operation
        self.key = key
        self.value = value
    def modify(self, record):
        """
        Modify the record inplace.
        Args:
            record (dict): Record to modify
        Returns:
            bool: True if the record was modified

            The Boolean returned is just used for control
            (pretty logs, better verbose information)
        Examples:
            >>> modification = Modification('SET', 'key', 'value')
            >>> modification.modify({})
            True
            >>> record
            {'key': 'value'}
        """
        return_code = False
        log.debug("Starting modification [{}, {}, {}]".format(self.operation, self.key, self.value))
        if self.operation == 'SET':
            return_code = bool(self.value and record.get(self.key) != self.value)
            record[self.key] = self.value
        if self.operation == 'SET_TEMPLATE':
            record[Template(self.key).render(record)] = Template(self.value).render(record)
            return_code = True
        if self.operation == 'ARRAY_APPEND':
            array = record.get(self.key)
            if array and type(array) == list:
                record[self.key] += self.value
                return_code = True
        if self.operation == 'ARRAY_DELETE':
            array = record.get(self.key)
            if array and type(array) == list and self.value in array:
                array.remove(self.value)
                return_code = True
        if self.operation == 'DELETE':
            if self.key in record:
                del record[self.key]
                return_code = True
        return return_code
