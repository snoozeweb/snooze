#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''Configuration and fixtures for testing'''

from logging import getLogger

import mongomock
import pytest
from falcon.testing import TestClient
from pytest_data.functions import get_data

from snooze.db.database import Database
from snooze.core import Core
from snooze.api.base import Api

log = getLogger('snooze')

@pytest.fixture(scope='module')
def config():
    return {
        'api': {'type': 'falcon'},
        'process_plugins': ['rule', 'aggregaterule', 'snooze', 'notification'],
        'database': {'type': 'mongo', 'host': 'localhost', 'port': 27017},
        'socket_path': './test.socket',
        'stats': False,
        'bootstrap_db': True,
        'init_sleep': 0,
    }

def write_data(db, request):
    '''Write data fetch with get_data(request, 'data') to a database'''
    data = get_data(request, 'data')
    for key, value in data.items():
        db.delete(key, [], True)
    for key, value in data.items():
        db.write(key, value)

@pytest.fixture(scope='function')
@mongomock.patch('mongodb://localhost:27017')
def db(config, request):
    db = Database(config.get('database'))
    write_data(db, request)
    return db

@pytest.fixture(scope='class')
@mongomock.patch('mongodb://localhost:27017')
def core(config):
    return Core(config)

@pytest.fixture(scope='class')
def api(core):
    return Api(core)

@pytest.fixture(scope='function')
@mongomock.patch('mongodb://localhost:27017')
def client(config, request):
    core = Core(config)
    data = get_data(request, 'data')
    log.debug("data: {}".format(data))
    write_data(core.db, request)
    api = Api(core)
    token = api.get_root_token()
    log.info("Token obtained from get_root_token: {}".format(token))
    headers = {'Authorization': 'JWT {}'.format(token)}
    return TestClient(api.handler, headers=headers)
