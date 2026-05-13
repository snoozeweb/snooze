#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''Configuration and fixtures for testing.

Every fixture that touches the database is parametrised over the two
supported backends — ``mongo`` (via ``mongomock``) and ``postgres`` (via
``testcontainers``). Tests that only make sense for one backend should
carry ``@pytest.mark.mongo_only`` or ``@pytest.mark.postgres_only``;
the ``db_backend`` fixture skips the wrong combinations automatically.
'''

import contextlib
from logging import getLogger
from socket import socket, AF_INET, SOCK_STREAM
from typing import Iterator

import mongomock
import pytest
import yaml
from falcon.testing import TestClient
from pytest_data.functions import get_data

from snooze.core import Core, MAIN_THREADS
from snooze.db.database import Database, get_database
from snooze.utils.config import Config

log = getLogger('tests')

BASE_CONFIG = {
    'core': {
        'socket_path': './test.socket',
        'init_sleep': 0,
        'stats': False,
        'backup': {'enabled': False},
        'ssl': {'enabled': False},
    },
}

_BACKENDS = ('mongo', 'postgres')


def write_data(db, request):
    '''Write data fetched with ``get_data(request, 'data')`` to a database,
    after dropping any pre-existing collections so each test starts clean.'''
    data = get_data(request, 'data', {})
    for collection in db.list_collections():
        db.drop(collection)
    for key, value in data.items():
        db.write(key, value)


# --------------------------------------------------------------------- #
# Backend selection                                                     #
# --------------------------------------------------------------------- #


@pytest.fixture(params=_BACKENDS, ids=_BACKENDS)
def db_backend(request) -> str:
    '''Parametrise every database-touching test over the supported
    backends. Tests can opt out per-backend via
    ``@pytest.mark.mongo_only`` / ``@pytest.mark.postgres_only``.'''
    backend = request.param
    if backend != 'mongo' and request.node.get_closest_marker('mongo_only'):
        pytest.skip('mongo-only test')
    if backend != 'postgres' and request.node.get_closest_marker('postgres_only'):
        pytest.skip('postgres-only test')
    return backend


@pytest.fixture(scope='session')
def pg_dsn() -> Iterator[str]:
    '''Spin up a single ``postgres:16-alpine`` container for the whole
    session via testcontainers. If Docker (or testcontainers) is
    unavailable, postgres-parametrised tests are skipped — Mongo tests
    keep working.'''
    try:
        from testcontainers.postgres import PostgresContainer
    except ImportError:
        pytest.skip('testcontainers not installed; install the test extras')
        return
    try:
        container = PostgresContainer('postgres:16-alpine')
        container.start()
    except Exception as err:  # pragma: no cover — environmental
        pytest.skip(f'Could not start Postgres container: {err}')
        return
    try:
        dsn = (
            f"host={container.get_container_host_ip()} "
            f"port={container.get_exposed_port(5432)} "
            f"dbname={container.dbname} "
            f"user={container.username} "
            f"password={container.password}"
        )
        log.info("Postgres testcontainer ready: %s", dsn)
        yield dsn
    finally:
        with contextlib.suppress(Exception):
            container.stop()


@pytest.fixture
def db_config(db_backend, request) -> dict:
    '''Return the ``core.database`` config block for the active backend.'''
    if db_backend == 'mongo':
        return {'type': 'mongo', 'host': 'localhost', 'port': 27017}
    dsn = request.getfixturevalue('pg_dsn')
    return {'type': 'postgres', 'dsn': dsn}


# --------------------------------------------------------------------- #
# Shared fixtures                                                       #
# --------------------------------------------------------------------- #


@pytest.fixture(name='port', scope='function')
def fixture_port() -> int:
    '''A fixture that returns an open port'''
    sock = socket(AF_INET, SOCK_STREAM)
    sock.bind(('', 0))
    port = sock.getsockname()[1]
    sock.close()
    return port


@pytest.fixture(name='config', scope='function')
def fixture_config(port, tmp_path, request, db_config) -> Config:
    '''Build a fresh :class:`Config` per test, with the database section
    populated by the active backend.'''
    base = {**BASE_CONFIG, 'core': {**BASE_CONFIG['core'], 'database': db_config}}
    configs = {**base, **get_data(request, 'configs', {})}
    configs['port'] = port

    for section, data in configs.items():
        path = tmp_path / f"{section}.yaml"
        path.write_text(yaml.dump(data), encoding='utf-8')

    return Config(tmp_path)


def _mongo_patch_ctx(db_backend: str):
    '''Return a context manager that patches pymongo when ``db_backend``
    is ``mongo``, and a no-op otherwise.'''
    if db_backend == 'mongo':
        return mongomock.patch('mongodb://localhost:27017')
    return contextlib.nullcontext()


def _drop_all(database: Database) -> None:
    '''Best-effort cleanup of every collection on the backend.'''
    try:
        for collection in database.list_collections():
            database.drop(collection)
    except Exception:  # pragma: no cover — safety net for teardown
        log.exception("Failed to drop collections during teardown")


@pytest.fixture(name='db', scope='function')
def fixture_db(config, request, db_backend) -> Iterator[Database]:
    '''Database fixture; mocked for Mongo, real container for Postgres.'''
    with _mongo_patch_ctx(db_backend):
        database = get_database(config.core.database)
        try:
            write_data(database, request)
            yield database
        finally:
            if db_backend == 'postgres':
                _drop_all(database)
                getattr(database, 'close', lambda: None)()


@pytest.fixture(name='core', scope='function')
def fixture_core(config, request, db_backend) -> Iterator[Core]:
    '''Core fixture; same backend selection logic as ``db``.'''
    allowed_threads = get_data(request, 'allowed_threads') or MAIN_THREADS
    with _mongo_patch_ctx(db_backend):
        core = Core(config.basedir, allowed_threads)
        try:
            write_data(core.db, request)
            yield core
        finally:
            if db_backend == 'postgres':
                _drop_all(core.db)
                getattr(core.db, 'close', lambda: None)()


@pytest.fixture(name='api', scope='function')
def fixture_api(core):
    '''Fixture returning an Api'''
    return core.api


@pytest.fixture(name='client', scope='function')
def fixture_client(api, request):
    '''Fixture returning a falcon TestClient'''
    token = api.get_root_token()
    log.debug("Token obtained from get_root_token: %s", token)
    headers = {'Authorization': f"JWT {token}"}
    client = TestClient(api.handler, headers=headers)
    data = get_data(request, 'data', {})
    for collection, items in data.items():
        api.core.db.drop(collection)
        client.simulate_post(f"/api/{collection}", json=items)
    return client
