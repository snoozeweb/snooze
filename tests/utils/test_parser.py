#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

from snooze.utils.parser import parser

class TestParserLogic:
    def test_word(self):
        result = parser('hello')
        assert result == ['SEARCH', 'hello']

    def test_key_value(self):
        result = parser('key = value')
        assert result == ['=', 'key', 'value']

    def test_and(self):
        result = parser('key1=value1 AND key2=value2')
        assert result == ['AND', ['=', 'key1', 'value1'], ['=', 'key2', 'value2']]

    def test_and_symbol(self):
        result = parser('key1=value1&key2=value2')
        assert result == ['AND', ['=', 'key1', 'value1'], ['=', 'key2', 'value2']]

    def test_implicit_and(self):
        result = parser('key1=value1 key2=value2')
        assert result == ['AND', ['=', 'key1', 'value1'], ['=', 'key2', 'value2']]

    def test_or(self):
        result = parser('key1=value1 OR key2=value2')
        assert result == ['OR', ['=', 'key1', 'value1'], ['=', 'key2', 'value2']]

    def test_or_symbol(self):
        result = parser('key1=value1|key2=value2')
        assert result == ['OR', ['=', 'key1', 'value1'], ['=', 'key2', 'value2']]

    def test_not(self):
        result = parser('not key1=value1')
        assert result == ['NOT', ['=', 'key1', 'value1']]

    def test_not_symbol(self):
        result = parser('!key1=value1')
        assert result == ['NOT', ['=', 'key1', 'value1']]

    def test_parenthesis(self):
        result = parser('NOT (key1=value1 AND key2=value2)')
        assert result == ['NOT', ['AND', ['=', 'key1', 'value1'], ['=', 'key2', 'value2']]]

    def test_priority(self):
        result = parser('NOT key1=value1 AND key2=value2')
        assert result == ['AND', ['NOT', ['=', 'key1', 'value1']], ['=', 'key2', 'value2']]

    def test_complex_query(self):
        result = parser('myapp and source=syslog and custom_field = "myapp01" or custom_field = "myapp02"')
        assert result == ['AND',
            ['SEARCH', 'myapp'],
            ['AND',
                ['=', 'source', 'syslog'],
                ['OR',
                    ['=', 'custom_field', 'myapp01'],
                    ['=', 'custom_field', 'myapp02'],
                ],
            ],
        ]


    def test_complex_query_parenthesis(self):
        result = parser('myapp and source=syslog and (custom_field = "myapp01" or custom_field = "myapp02")')
        assert result == ['AND',
            ['SEARCH', 'myapp'],
            ['AND',
                ['=', 'source', 'syslog'],
                ['OR',
                    ['=', 'custom_field', 'myapp01'],
                    ['=', 'custom_field', 'myapp02'],
                ]
            ],
        ]

class TestParserTypes:
    def test_integer(self):
        result = parser('123')
        assert result == ['SEARCH', 123]

    def test_negative_integer(self):
        result = parser('-42')
        assert result == ['SEARCH', -42]

    def test_float(self):
        result = parser('3.14')
        assert result == ['SEARCH', 3.14]

    def test_bool_true(self):
        result = parser('mybool=true')
        assert result == ['=', 'mybool', True]

    def test_bool_false(self):
        result = parser('mybool=false')
        assert result == ['=', 'mybool', False]

    def test_double_quoted_string(self):
        result = parser('key = "value"')
        assert result == ['=', 'key', 'value']

    def test_single_quoted_string(self):
        result = parser("key = 'value'")
        assert result == ['=', 'key', 'value']

    def test_double_quoted_escape(self):
        result = parser(r'key = "value\t\n\\"')
        assert result == ['=', 'key', "value\t\n\\"]

    def test_double_quoted_escape_quote(self):
        data = r'key = "my \"test\""'
        result = parser(data)
        print(data)
        print(result)
        assert result == ['=', 'key', 'my "test"']

    def test_quoted_field(self):
        result = parser('"myfield with space" = myapp01')
        assert result == ['=', 'myfield with space', 'myapp01']

    def test_single_quote_string(self):
        result = parser("'myfield' = 'myvalue'")
        assert result == ['=', 'myfield', 'myvalue']

    def test_array(self):
        result = parser('myfield = [1, 2, 3]')
        assert result == ['=', 'myfield', [1, 2, 3]]

    def test_nested_array(self):
        result = parser('myfield = [[1], [2], [3]]')
        assert result == ['=', 'myfield', [[1], [2], [3]]]

    def test_dict(self):
        result = parser('myfield = {a: 1, b: 2}')
        assert result == ['=', 'myfield', {'a': 1, 'b': 2}]

    def test_nested_dict(self):
        result = parser('myfield = {a: {"mymessage": "x"}, b: 2}')
        assert result == ['=', 'myfield', {'a': {'mymessage': 'x'}, 'b': 2}]

    def test_hash(self):
        result = parser('hash=3f75728488a0e6892905f0db6a473382')
        assert result == ['=', 'hash', '3f75728488a0e6892905f0db6a473382']

class TestParserOperations:
    def test_nequal(self):
        result = parser('process != systemd')
        assert result == ['!=', 'process', 'systemd']

    def test_matches(self):
        result = parser('message MATCHES "[aA]lert"')
        assert result == ['MATCHES', 'message', '[aA]lert']

    def test_matches_symbol(self):
        result = parser('message ~ "[aA]lert"')
        assert result == ['MATCHES', 'message', '[aA]lert']

    def test_exists(self):
        result = parser('custom_field EXISTS')
        assert result == ['EXISTS', 'custom_field']

    def test_exists_symbol(self):
        result = parser('custom_field?')
        assert result == ['EXISTS', 'custom_field']

    def test_exists_symbol_not(self):
        result = parser('!custom_field?')
        assert result == ['NOT', ['EXISTS', 'custom_field']]

    def test_gt(self):
        result = parser('mail_queue>100')
        assert result == ['>', 'mail_queue', 100]

    def test_lt(self):
        result = parser('port < 1024')
        assert result == ['<', 'port', 1024]

    def test_contains(self):
        result = parser('rules contains myrule')
        assert result == ['CONTAINS', 'rules', 'myrule']

    def test_contains_array(self):
        result = parser('myarray contains [1, 2, 3]')
        assert result == ['CONTAINS', 'myarray', [1, 2, 3]]

    def test_in(self):
        result = parser('myrule in rules')
        assert result == ['IN', 'myrule', 'rules']

    def test_in_array(self):
        result = parser('[1, 2, 3] in myarray')
        assert result == ['IN', [1, 2, 3], 'myarray']
