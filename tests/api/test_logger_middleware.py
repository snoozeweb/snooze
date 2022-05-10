#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

import falcon
import pytest
from falcon.testing import TestClient

from snooze.api import LoggerMiddleware

class TestRoute:
    def on_get(self, req, resp):
        resp.media = {'result': 'Hello, world!'}

@pytest.fixture(scope='function')
def client():
    conf = {'audit_excluded_paths': ['/api/patlite', '/metrics', '/web']}
    api = falcon.App(middleware=[LoggerMiddleware(conf)])
    api.add_route('/test', TestRoute())
    return TestClient(api)

class TestLoggerMiddleware:
    def test_process_message(self, client):
        client.simulate_get('/test')
