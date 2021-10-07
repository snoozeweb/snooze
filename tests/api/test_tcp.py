#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''Test if TCP server is working'''

import time
from socket import socket, AF_INET, SOCK_STREAM

import falcon
import requests
import pytest

from snooze.api.tcp import WSGITCPServer

def get_open_port():
    s = socket(AF_INET, SOCK_STREAM)
    s.bind(('', 0))
    port = s.getsockname()[1]
    s.close()
    return port

class TestRoute:
    def on_get(self, req, resp):
        resp.media = {'result': 'Hello, world!'}

@pytest.fixture(scope='function')
def wsgiserver():
    port = get_open_port()
    conf = {'listen_addr': '0.0.0.0', 'port': port}
    api = falcon.App()
    api.add_route('/test', TestRoute())
    thread = WSGITCPServer(conf, api)
    thread.daemon = True
    thread.start()
    time.sleep(0.1)
    return thread, port

def test_wsgiserver(wsgiserver):
    thread, port = wsgiserver
    resp = requests.get("http://localhost:{}/test".format(port))
    assert resp.status_code == 200
    assert resp.json() == {'result': 'Hello, world!'}
    thread.stop()
