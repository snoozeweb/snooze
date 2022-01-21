#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#
'''
Objects and utils for representing conditions.
'''

import re
from abc import abstractmethod, ABC
from logging import getLogger

from snooze.utils.functions import dig, flatten

LOG = getLogger('snooze.condition')

SCALARS = (str, int, float)

# Functions
def search(record, field):
    '''Searching a record while supporting . in field'''
    return dig(record, *field.split('.'))

def unsugar_regex(regex):
    '''Remove the leading and ending `/` if they are both present'''
    if len(regex) > 0 and regex[0] == '/' and regex[-1] == '/':
        regex = regex[1:-1]
    return regex

def lazy_search(regex, string):
    '''Attempt to regex search a word in a string'''
    try:
        return re.search(regex, string, flags=re.IGNORECASE)
    except TypeError:
        return False

def convert_float(value1, value2):
    '''Attempt to convert 2 values into float, else give up
    and return the original values. Used for comparisons.'''
    try:
        return float(value1), float(value2)
    except ValueError:
        return value1, value2

# Exceptions
class OperationNotSupported(Exception):
    '''Exception raised when the condition requested doesn't exist'''
    def __init__(self, name):
        message = f"Condition '{name}' is not supported"
        super().__init__(message)

class ConditionInvalid(RuntimeError):
    '''Exception raised when there was an error when creating a condition,
    usually due to invalid inputs incompatible with the condition type'''
    def __init__(self, name, args, err):
        message = f"Error in condition '{name}' ({args}): {err}"
        super().__init__(message)

# Classes
class Condition(ABC):
    '''An abstract class for all conditions'''
    def __init__(self, args):
        self._args = args
        LOG.debug("Instantiating %s(%s)", self.__class__.__name__, args)
        try:
            self.operator = args[0]
        except IndexError as err:
            raise ConditionInvalid('UNKNOWN', args, err) from err

    @abstractmethod
    def match(self, record):
        '''Return true if the record match the condition'''

    def to_list(self):
        '''Return the list representation of a condition'''
        return self._args

    def __and__(self, other):
        return And(['AND', self.to_list(), other.to_list()])

    def __or__(self, other):
        return Or(['OR', self.to_list(), other.to_list()])

    def __invert__(self):
        return Not(['NOT', self.to_list()])

class BinaryOperator(Condition):
    '''An abstract class to wrap binary operators'''
    display_name = None
    def __init__(self, args):
        super().__init__(args)
        try:
            self.field = args[1]
            self.value = args[2]
            if not (isinstance(self.field, str) and len(self.field) > 0):
                raise ConditionInvalid(args[0], args, "Field is not a valid non-null string")
        except IndexError as err:
            raise ConditionInvalid(args[0], args, err) from err

    def __str__(self):
        op_name = self.display_name or self.operator.lower()
        return f"({self.field} {op_name} {repr(self.value)})"

class AlwaysTrue(Condition):
    '''A condition that always return True for matching'''
    def __init__(self, *_args):
        super().__init__([''])
        self._args = []
    def match(self, record):
        return True
    def __str__(self):
        return '()'

# Logic
class Not(Condition):
    '''Match the opposite of a given condition'''
    def __init__(self, args):
        super().__init__(args)
        self.condition = get_condition(args[1])
    def match(self, record):
        return not self.condition.match(record)
    def __str__(self):
        return '!' + str(self.condition)

class And(Condition):
    '''Match only if the two conditions given in arguments match'''
    def __init__(self, args):
        super().__init__(args)
        self.left = get_condition(args[1])
        self.right = get_condition(args[2])
    def match(self, record):
        return self.left.match(record) and self.right.match(record)
    def __str__(self):
        return f"({self.left} & {self.right})"

class Or(Condition):
    '''Match only if one of the two condition given in arguments match'''
    def __init__(self, args):
        super().__init__(args)
        self.left = get_condition(args[1])
        self.right = get_condition(args[2])
    def match(self, record):
        return self.left.match(record) or self.right.match(record)
    def __str__(self):
        return f"({self.left} | {self.right})"

# Basic operations
class Equals(BinaryOperator):
    '''Match if the field of a record is exactly equal to a given value'''
    display_name = '='
    def match(self, record):
        return search(record, self.field) == self.value

class NotEquals(BinaryOperator):
    '''Match if a field of a record is not equal to a given value'''
    display_name = '!='
    def match(self, record):
        record_value = search(record, self.field)
        return (
            record_value is not None
            and record_value != self.value
        )

class GreaterThan(BinaryOperator):
    '''Match if the field of a record is strictly greater than a value.
    Will attempt to convert both value to float, so integers/floats as string
    can be compared to real integer/float inside the record.'''
    display_name = '>'
    def match(self, record):
        try:
            record_value = search(record, self.field)
            value, record_value = convert_float(self.value, record_value)
            return record_value > value
        except TypeError: # Cannot be compared
            return False

class LowerThan(BinaryOperator):
    '''Match if the field of a record is strictly lower than a value.
    Will attempt to convert both value to float, so integers/floats as string
    can be compared to real integer/float inside the record.'''
    display_name = '<'
    def match(self, record):
        try:
            record_value = search(record, self.field)
            value, record_value = convert_float(self.value, record_value)
            return record_value < value
        except TypeError: # Cannot be compared
            return False

class GreaterOrEquals(BinaryOperator):
    '''Match if the field of a record is greater than or equal to a value.
    Will attempt to convert both value to float, so integers/floats as string
    can be compared to real integer/float inside the record.'''
    display_name = '>='
    def match(self, record):
        try:
            record_value = search(record, self.field)
            value, record_value = convert_float(self.value, record_value)
            return record_value >= value
        except TypeError: # Cannot be compared
            return False

class LowerOrEquals(BinaryOperator):
    '''Match if the field of a record is lower than or equal a value.
    Will attempt to convert both value to float, so integers/floats as string
    can be compared to real integer/float inside the record.'''
    display_name = '<='
    def match(self, record):
        try:
            record_value = search(record, self.field)
            value, record_value = convert_float(self.value, record_value)
            return record_value <= value
        except TypeError: # Cannot be compared
            return False

# Complex operations
class Matches(BinaryOperator):
    '''Match if the field of a record match a given regex. The regex can optionally
    start and end with `/`, to make it easier to spot in configuration. The regex method
    used is a search (`re.search`), so for strict matches, the `^` and `$` need to be used.
    '''
    display_name = '~'
    def __init__(self, args):
        super().__init__(args)
        self.field = args[1]
        value = unsugar_regex(args[2])
        self.regex = re.compile(value)
    def match(self, record):
        record_value = search(record, self.field)
        if record_value is None:
            return False
        return self.regex.search(record_value) is not None

class Exists(Condition):
    '''Match if a given field exist and is not null in the record'''
    def __init__(self, args):
        super().__init__(args)
        self.field = args[1]
    def match(self, record):
        return search(record, self.field) is not None
    def __str__(self):
        return self.field + '?'

class Search(Condition):
    '''Search a given string in the key/values of a record (stringify the record and
    search in the string)'''
    def __init__(self, args):
        super().__init__(args)
        self.value = args[1]
    def match(self, record):
        return self.value in str(record)
    def __str__(self):
        return f"(SEARCH {repr(self.value)})"

class Contains(BinaryOperator):
    '''Match if it finds a given word/regex in a flatten list of object, or in a string'''
    display_name = 'contains'
    def match(self, record):
        record_value = search(record, self.field)
        try:
            return any(
                lazy_search(value, rec)
                for value in flatten([self.value])
                for rec in flatten([record_value])
            )
        except TypeError:
            return False

class In(Condition):
    '''Match if a record field is in a given list of objects, or if
    the record field has any item matching a given condition.
    '''
    def __init__(self, args):
        super().__init__(args)
        self.field = args[2]
        self.value = args[1]
        if self.is_condition():
            self.mode = 'condition'
            self.condition = get_condition(self.value)
        else:
            self.mode = 'list'

    def is_condition(self):
        '''Detect if the provided argument is a condition or a scalar'''
        try:
            return self.value[0] in CONDITIONS
        except IndexError:
            return False

    def match(self, record):
        record_value = search(record, self.field)
        if self.mode == 'condition':
            return any(
                self.condition.match(rec)
                for rec in record_value
            )
        if self.mode == 'list':
            return any(
                rec in flatten([self.value])
                for rec in record_value
            )
        # Unknown case
        LOG.warning("Unknown situation encountered for IN condition: condition=%s, record=%s",
            self._args, record)
        return False

    def __str__(self):
        if self.mode == 'condition':
            return f"({self.condition} in {self.field})"
        if self.mode == 'list':
            return f"({repr(self.value)} in {self.field})"
        return "???"

CONDITIONS = {
    'AND': And,
    'OR': Or,
    'NOT': Not,
    'EXISTS': Exists,
    'CONTAINS': Contains,
    'IN': In,
    '=': Equals,
    '!=': NotEquals,
    'MATCHES': Matches,
    '>=': GreaterOrEquals,
    '<=': LowerOrEquals,
    '>': GreaterThan,
    '<': LowerThan,
    'SEARCH': Search,
    '': AlwaysTrue,
    None: AlwaysTrue,
}

def validate_condition(obj):
    '''Validate the condition of an object'''
    condition = obj.get('condition')
    if condition:
        get_condition(condition)

def get_condition(args):
    '''Return an instance of a condition given a condition array representation'''
    try:
        name = args[0]
        condition_class = CONDITIONS[name]
        return condition_class(args)
    except IndexError:
        return AlwaysTrue()
    except KeyError as err:
        raise OperationNotSupported(name) from err
    except TypeError as err:
        raise ConditionInvalid(name, args, err) from err
