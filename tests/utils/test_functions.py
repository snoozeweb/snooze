#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

from collections import defaultdict
from unittest.mock import Mock

import pytest
import falcon
from falcon import Request

from snooze.utils.functions import *
from snooze.utils.typing import *

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

@pytest.fixture(scope='function')
def req():
    '''A fixture returning a dummy request object'''
    env = defaultdict(lambda: "")
    request = Request(env)
    # Safe defaults
    request.path = '/'
    request.method = 'GET'
    return request

@pytest.fixture(scope='function')
def route():
    '''A fixture that returns a mock of BasicRoute that
    will answer all call is_authorized makes with default options'''
    route = Mock()
    route.core.config.core.no_login = False
    route.plugin.name = 'myplugin'
    route.options.authorization_policy = AuthorizationPolicy()
    return route

class TestIsAuthorized:
    def test_get_plugin_name(self, req, route):
        req.context.auth = AuthPayload(username='test', method='local', permissions=['ro_myplugin'])
        assert is_authorized(route, req) == True

    def test_post_plugin_name(self, req, route):
        req.method = 'POST'
        req.context.auth = AuthPayload(username='test', method='local', permissions=['rw_myplugin'])
        assert is_authorized(route, req) == True

    def test_post_read_only(self, req, route):
        req.method = 'POST'
        req.context.auth = AuthPayload(username='test', method='local', permissions=['ro_myplugin'])
        assert is_authorized(route, req) == False

    def test_root_power(self, req, route):
        req.method = 'POST'
        req.context.auth = AuthPayload(username='root', method='root')
        assert is_authorized(route, req) == True

    def test_no_login(self, req, route):
        route.core.config.core.no_login = True
        req.context.auth = AuthPayload(username='test', method='local')
        assert is_authorized(route, req) == True

    def test_route_any(self, req, route):
        route.options.authorization_policy = AuthorizationPolicy(read={'any'})
        req.context.auth = AuthPayload(username='test', method='local')
        assert is_authorized(route, req) == True

    def test_route_custom(self, req, route):
        route.options.authorization_policy = AuthorizationPolicy(read={'custom_permission'})
        req.context.auth = AuthPayload(username='test', method='local', permissions={'custom_permission'})
        assert is_authorized(route, req) == True

    def test_route_rw_all(self, req, route):
        req.context.auth = AuthPayload(username='test', method='local', permissions={'rw_all'})
        assert is_authorized(route, req) == True

    def test_unknown_method(self, req, route):
        req.method = 'OPTIONS'
        req.context.auth = AuthPayload(username='test', method='local', permissions={'rw_all'})
        assert is_authorized(route, req) == False
