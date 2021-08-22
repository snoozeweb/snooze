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
        templated_key = self.key
        templated_value = self.value
        if self.key and type(self.key) is str:
            templated_key = Template(self.key).render(record)
        if self.value and type(self.value) is str:
            templated_value = Template(self.value).render(record)
        if self.operation == 'SET':
            return_code = bool(templated_value and record.get(templated_key) != templated_value)
            record[templated_key] = templated_value
        elif self.operation == 'ARRAY_APPEND':
            array = record.get(templated_key)
            if array and type(array) == list:
                record[templated_key] += templated_value
                return_code = True
        elif self.operation == 'ARRAY_DELETE':
            array = record.get(templated_key)
            if array and type(array) == list and templated_value in array:
                array.remove(templated_value)
                return_code = True
        elif self.operation == 'DELETE':
            if templated_key in record:
                del record[templated_key]
                return_code = True
        return return_code
