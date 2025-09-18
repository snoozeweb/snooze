"""Combinatory parser for the query language"""

import pyparsing as pp
from pyparsing import pyparsing_common as ppc, restOfLine

pp.ParserElement.enablePackrat()

EQUAL = pp.Literal("=").setParseAction(lambda: "SET")
DELETE = (
    pp.CaselessKeyword("DELETE") | pp.CaselessKeyword("DEL") | pp.Literal("~")
).setParseAction(lambda: "DELETE")
ARRAY_APPEND = (pp.Literal("<<") | pp.Literal("+")).setParseAction(
    lambda: "ARRAY_APPEND"
)
ARRAY_DELETE = pp.Literal("-").setParseAction(lambda: "ARRAY_DELETE")

LPAR, RPAR, LBRACK, RBRACK, LBRACE, RBRACE, COLON = map(pp.Suppress, "()[]{}:")

AND = pp.Optional(pp.CaselessKeyword("AND") | "&").setParseAction(lambda: "AND")

valid_word = pp.Regex(r"[a-zA-Z0-9_.-]+")

string = pp.QuotedString('"') | pp.QuotedString("'")
boolean = pp.CaselessKeyword("true").setParseAction(lambda: True) | pp.CaselessKeyword(
    "false"
).setParseAction(lambda: False)

literal = pp.Forward()

array_elements = pp.delimitedList(literal, delim=",")
array = pp.Group(LBRACK + pp.Optional(array_elements, []) + RBRACK)
array.setParseAction(lambda t: t.asList())
hashmap = pp.Forward()

fieldname = string | valid_word
literal << (
    ppc.real ^ ppc.signed_integer ^ string ^ array ^ hashmap ^ boolean ^ valid_word
)

hashmap_element = pp.Group(fieldname + COLON + literal)
hashmap_elements = pp.delimitedList(hashmap_element, delim=",")
hashmap << pp.Dict(LBRACE + pp.Optional(hashmap_elements) + RBRACE)
hashmap.setParseAction(lambda t: t.asDict())

term = pp.Forward()
expression = pp.Forward()

operation = EQUAL | ARRAY_APPEND | ARRAY_DELETE
field_operation = DELETE

term << (
    (fieldname("field") + operation("operation") + literal("value"))
    | (field_operation("operation") + fieldname("field"))
)


class Term:
    def __init__(self, tokens):
        if "field" in tokens:
            self.field = tokens["field"]
            self.operation = tokens["operation"]
        else:
            self.field = None
            self.operation = None
        if "value" in tokens:
            self.value = tokens["value"]
        else:
            self.value = None

    def __repr__(self):
        if self.field and self.value:
            return "Term({}, {})".format(self.operation, [self.field, self.value])

    def asList(self):
        if self.field is not None and self.value is not None:
            return [[self.operation, self.field, self.value]]
        elif self.field is not None:
            return [[self.operation, self.field]]


class Operation:
    def __init__(self, tokens):
        tokens = tokens[0]
        if len(tokens) > 1:
            if tokens[1] == "AND":
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
                args += arg.asList()
            elif isinstance(arg, Term):
                args += arg.asList()
            else:
                args.append(arg)
        return args


class Comment:
    def __init__(self, tokens):
        self.expression = tokens["exp"]
        if "value" in tokens:
            self.restofline = tokens["value"]
        else:
            self.restofline = None

    def __repr__(self):
        return "Expression({}, {})".format(self.expression, self.restofline)

    def asList(self):
        return (self.expression.asList(), self.restofline)


term.setParseAction(Term)

# Parse expressions that have an order of priority in operations
expression << pp.infixNotation(
    term,
    [
        (AND, 2, pp.opAssoc.LEFT, Operation),
    ],
)

with_comment = pp.Forward()
with_comment << (expression("exp") + pp.Optional(pp.empty + restOfLine("value")))

with_comment.setParseAction(Comment)


def parser(data):
    result = with_comment.parseString(data).asList()[0].asList()
    return result
