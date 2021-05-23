#!/usr/bin/env python

import os
import pytest
from falcon_auth import JWTAuthBackend
from requests_unixsocket import Session
from urllib.parse import quote

from snooze.api.socket import SocketServer
from snooze.api.socket import prepare_socket

import json
import threading
import jwt

def test_prepare_socket():
    my_socket = prepare_socket('./test1.socket')
    pwd = os.path.abspath('.')
    assert my_socket == pwd + '/test1.socket'

@pytest.fixture(scope='class')
def mysocket():
    jwt_auth = JWTAuthBackend(lambda u: u, 'secret')
    s = SocketServer(jwt_auth, socket_path='./test2.socket')
    thread = threading.Thread(target=s.serve)
    thread.daemon = True
    thread.start()
    return s

def test_socket_existence(mysocket):
    assert os.path.exists('./test2.socket')

def test_socket_connection(mysocket):
    path = os.path.abspath('./test2.socket')
    response = Session().get("http+unix://{}/root_token".format(quote(path, safe='')))
    assert response

def test_socket_root_token(mysocket):
    path = os.path.abspath('./test2.socket')
    response = Session().get("http+unix://{}/root_token".format(quote(path, safe='')))
    try:
        myjson = json.loads(response.content)
    except ValueError:
        assert False
    root_token = myjson.get('root_token')
    assert root_token
    token = jwt.decode(jwt=root_token, key='secret')
    assert token.get('user') == {'name': 'root'}
