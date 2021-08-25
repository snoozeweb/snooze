'''Combinatory parser for the query language'''

import pyparsing as pp
from pyparsing import pyparsing_common as ppc

pp.ParserElement.enablePackrat()

EQUAL = pp.Literal('=')
NOT_EQUAL = pp.Literal('!=')
GT = pp.Literal('>')
LT = pp.Literal('<')
MATCHES = (pp.CaselessKeyword('MATCHES') | pp.Literal('~')).setParseAction(lambda: 'MATCHES')
EXISTS = (pp.CaselessKeyword('EXISTS') | pp.Literal('?')).setParseAction(lambda: 'EXISTS')
CONTAINS = (pp.CaselessKeyword('CONTAINS')).setParseAction(lambda: 'CONTAINS')
IN = (pp.CaselessKeyword('IN')).setParseAction(lambda: 'IN')

LPAR, RPAR, LBRACK, RBRACK, LBRACE, RBRACE, COLON = map(pp.Suppress, '()[]{}:')

NOT = (pp.CaselessKeyword('NOT') | '!').setParseAction(lambda: 'NOT')
OR = (pp.CaselessKeyword('OR') | '|').setParseAction(lambda: 'OR')
AND = pp.Optional(pp.CaselessKeyword('AND') | '&').setParseAction(lambda: 'AND')

valid_word = pp.Regex(r'[a-zA-Z0-9_.-]+')

string = pp.QuotedString('"', escChar="\\") | pp.QuotedString("'", escChar="\\")
boolean = (
    pp.CaselessKeyword('true').setParseAction(lambda: True)
    | pp.CaselessKeyword('false').setParseAction(lambda: False)
)

literal = pp.Forward()

array_elements = pp.delimitedList(literal, delim=',')
array = pp.Group(LBRACK + pp.Optional(array_elements, []) + RBRACK)
array.setParseAction(lambda t: t.asList())
hashmap = pp.Forward()

fieldname = string | valid_word
literal << (ppc.real ^ ppc.signed_integer ^ string ^ array ^ hashmap ^ boolean ^ valid_word)

hashmap_element = pp.Group(fieldname + COLON + literal)
hashmap_elements = pp.delimitedList(hashmap_element, delim=',')
hashmap << pp.Dict(LBRACE + pp.Optional(hashmap_elements) + RBRACE)
hashmap.setParseAction(lambda t: t.asDict())

term = pp.Forward()
expression = pp.Forward()

operation = EQUAL | NOT_EQUAL | MATCHES | GT | LT | CONTAINS
field_operation = EXISTS
reverse_op = IN

term << (
    (fieldname('field') + operation('operation') + literal('value'))
    | (fieldname('field') + field_operation('operation'))
    | literal('field') + reverse_op('operation') + fieldname('value')
    | literal('value')
)

class Term:
    def __init__(self, tokens):
        if 'field' in tokens:
            self.field = tokens['field']
            self.operation = tokens['operation']
        else:
            self.field = None
            self.operation = None
        if 'value' in tokens:
            self.value = tokens['value']
        else:
            self.value = None
    def __repr__(self):
        if self.field and self.value:
            return "Term({}, {})".format(self.operation, [self.field, self.value])
        else:
            return "Term({})".format(self.value)
    def asList(self):
        if self.field is not None and self.value is not None:
            return [self.operation, self.field, self.value]
        elif self.field is not None:
            return [self.operation, self.field]
        elif self.value is not None:
            return ['SEARCH', self.value]

class Operation:
    def __init__(self, tokens):
        tokens = tokens[0]
        if len(tokens) > 1:
            if tokens[0] == 'NOT':
                self.op = 'NOT'
                if tokens[1] == 'NOT':
                    self.args = [Operation([tokens[1:]])]
                else:
                    self.args = [tokens[1]]
            elif tokens[1] in ['AND', 'OR']:
                self.op = tokens[1]
                if len(tokens) > 3:
                    self.args = [tokens[0], Operation([tokens[2:]])]
                else:
                    self.args = [tokens[0], tokens[2]]
        else:
            raise Exception("Unexpected operation: {}".format(tokens))
    def __repr__(self):
        return "Operation({}, {})".format(self.op, self.args)
    def asList(self):
        args = []
        for arg in self.args:
            if isinstance(arg, Operation):
                args.append(arg.asList())
            elif isinstance(arg, Term):
                args.append(arg.asList())
            else:
                args.append(arg)
        return [self.op, *args]

term.setParseAction(Term)

# Parse expressions that have an order of priority in operations
expression << pp.infixNotation(
    term,
    [
        (NOT, 1, pp.opAssoc.RIGHT, Operation),
        (OR, 2, pp.opAssoc.LEFT, Operation),
        (AND, 2, pp.opAssoc.LEFT, Operation),
    ],
)

def parser(data):
    result = expression.parseString(data).asList()[0].asList()
    return result
