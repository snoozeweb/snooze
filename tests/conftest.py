#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''Configuration and fixtures for testing'''

from logging import getLogger
from socket import socket, AF_INET, SOCK_STREAM

import mongomock
import pytest
import yaml
from falcon.testing import TestClient
from pytest_data.functions import get_data

from snooze.db.database import Database
from snooze.core import Core
from snooze.utils.config import Config

log = getLogger('tests')

DEFAULT_CONFIG = {
    'core': {
        'database': {'type': 'mongo', 'host': 'localhost', 'port': 27017},
        'socket_path': './test.socket',
        'init_sleep': 0,
        'stats': False,
        'backup': {'enabled': False},
        'ssl': {'enabled': False},
    },
}

def write_data(db, request):
    '''Write data fetch with get_data(request, 'data') to a database'''
    data = get_data(request, 'data', {})
    for collection in db.db.list_collection_names():
        db.db[collection].drop()
    for key, value in data.items():
        db.write(key, value)

@pytest.fixture(name='port', scope='function')
def fixture_port() -> int:
    '''A fixture that returns an open port'''
    sock = socket(AF_INET, SOCK_STREAM)
    sock.bind(('', 0))
    port = sock.getsockname()[1]
    sock.close()
    return port

@pytest.fixture(name='config', scope='function')
def fixture_config(port, tmp_path, request) -> Config:
    '''Fixture for writable configuration files returning a Config'''
    configs = {**DEFAULT_CONFIG, **get_data(request, 'configs', {})}
    configs['port'] = port

    for section, data in configs.items():
        path = tmp_path / f"{section}.yaml"
        path.write_text(yaml.dump(data), encoding='utf-8')

    return Config(tmp_path)

@pytest.fixture(name='db', scope='function')
@mongomock.patch('mongodb://localhost:27017')
def fixture_db(config, request) -> Database:
    '''Fixture returning a mocked mongodb Database'''
    database = Database(config.core.database)
    write_data(database, request)
    return database

@pytest.fixture(name='core', scope='function')
@mongomock.patch('mongodb://localhost:27017')
def fixture_core(config, request) -> Core:
    '''Fixture returning a Core'''
    core = Core(config.basedir)
    write_data(core.db, request)
    return core

@pytest.fixture(name='api', scope='function')
def fixture_api(core):
    '''Fixture returning an Api'''
    return core.api

@pytest.fixture(name='client', scope='function')
def fixture_client(api):
    '''Fixture returning a falcon TestClient'''
    token = api.get_root_token()
    log.debug("Token obtained from get_root_token: %s", token)
    headers = {'Authorization': f"JWT {token}"}
    return TestClient(api.handler, headers=headers)

