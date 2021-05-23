#!/usr/bin/python3.6

from snooze.core import Core
from snooze.api.base import Api
from falcon import testing

import mongomock
import json
import pytest

from logging import getLogger
log = getLogger('snooze.tests.api')

import yaml
from base64 import b64encode

from hashlib import sha256

with open('./examples/default_config.yaml', 'r') as f:
    default_config = yaml.load(f.read())

@mongomock.patch('mongodb://localhost:27017')
def test_basic_auth():
    core = Core(default_config)
    api = Api(core)
    # User
    username = 'myuser'
    password = 'secretpassphrase'
    password_hash = sha256(password.encode('utf-8')).hexdigest()
    users = [{'name': username, 'password': password_hash}]
    core.db.write('user', users)
    token = str(b64encode("{}:{}".format(username, password).encode('utf-8')), 'utf-8')
    headers = {'Authorization': 'Basic {}'.format(token)}
    log.debug(headers)
    client = testing.TestClient(api.handler, headers=headers)
    log.debug('Attempting Basic auth')
    result = client.simulate_get('/auth/basic').json
    log.debug("Received {}".format(result))
    assert result['token']

