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
    }

@pytest.fixture(scope='class')
@mongomock.patch('mongodb://localhost:27017')
def db(config, request):
    db = Database(config.get('database'))
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
    for key, value in data.items():
        core.db.delete(key, [], True)
    for key, value in data.items():
        core.db.write(key, value)
    for plugin in core.plugins:
        plugin.reload_data()
    api = Api(core)
    token = api.get_root_token()
    log.info("Token obtained from get_root_token: {}".format(token))
    headers = {'Authorization': 'JWT {}'.format(token)}
    return TestClient(api.handler, headers=headers)
