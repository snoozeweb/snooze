#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

from snooze.utils.functions import dig, ensure_kv, sanitize, flatten

def test_dig():
    dic = {
        'a': {
            'b': {
                'c': 'found'
            }
        }
    }
    assert dig(dic, 'a', 'b', 'c') == 'found'

def test_ensure_kv():
    dic = {'a': {'b': ''}}
    ensure_kv(dic, 'found', 'a', 'c', 'd')
    assert dic['a']['c']['d'] == 'found'

def test_sanitize():
    dic = {'a.b': {'c.d': 0}}
    assert sanitize(dic) == {'a_b': {'c_d': 0}}

def test_flatten():
    a = [1,[2,[3,[4,[5]]]]]
    assert flatten(a) == [1, 2, 3, 4, 5]
