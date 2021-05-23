import threading
import json
import jwt
import pytest
import os
import re

from falcon_auth import JWTAuthBackend
from click.testing import CliRunner

from snooze.cli.__main__ import snooze
from snooze.api.socket import SocketServer

def test_root_token_fail():
    runner = CliRunner()
    result = runner.invoke(snooze, ['root-token'])
    assert result.exit_code == 0
    assert result.output == "Could not find any socket in ['/var/run/snooze/socket', './snooze.socket']\n"

@pytest.fixture(scope='class')
def mysocket():
    jwt_auth = JWTAuthBackend(lambda u: u, 'secret')
    s = SocketServer(jwt_auth, socket_path='./test2.socket')
    thread = threading.Thread(target=s.serve)
    thread.daemon = True
    thread.start()
    return s

def test_root_token(mysocket):
    path = os.path.abspath('./test2.socket')
    runner = CliRunner()
    result = runner.invoke(snooze, ['root-token', '--socket', path])
    assert result.exit_code == 0
    assert re.match('Root token: .*', result.output)
