#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

#!/usr/bin/env python

import os
import pytest
import time
from requests_unixsocket import Session
from urllib.parse import quote

from snooze.api.socket import WSGISocketServer, admin_api
from snooze.token import TokenEngine

import json
import threading
import jwt

@pytest.fixture(scope='class')
def mysocket():
    token_engine = TokenEngine('secret')
    api = admin_api(token_engine)
    thread = WSGISocketServer(api, './test_socket.socket')
    thread.daemon = True
    thread.start()
    time.sleep(0.1)
    return thread

def test_socket_existence(mysocket):
    assert os.path.exists('./test_socket.socket')

def test_socket_connection(mysocket):
    path = os.path.abspath('./test_socket.socket')
    response = Session().get("http+unix://{}/api/root_token".format(quote(path, safe='')))
    assert response

def test_socket_root_token(mysocket):
    path = os.path.abspath('./test_socket.socket')
    response = Session().get("http+unix://{}/api/root_token".format(quote(path, safe='')))
    try:
        myjson = json.loads(response.content)
    except ValueError:
        assert False
    root_token = myjson.get('root_token')
    assert root_token
    payload = jwt.decode(jwt=root_token, options={'verify_signature': False})
    assert payload['username'] == 'root'
    assert payload['method'] == 'root'
    assert payload['permissions'] == ['rw_all']
