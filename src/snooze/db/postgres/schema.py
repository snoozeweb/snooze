#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#
'''Lazy table & index bootstrap for the Postgres backend.

Collections are created on first use, mirroring MongoDB's implicit
collection creation. The per-collection table has a fixed shape:

    snooze_<collection>(uid TEXT PRIMARY KEY,
                        data JSONB NOT NULL,
                        updated_at TIMESTAMPTZ NOT NULL DEFAULT now())

A GIN index on the JSONB column accelerates containment queries; one
B-tree index on ``updated_at`` accelerates the cleanup paths.'''

import re
from logging import getLogger
from threading import Lock
from typing import Set

from psycopg import Connection, sql

log = getLogger('snooze.db.postgres')

_TABLE_RE = re.compile(r'^[A-Za-z_][A-Za-z0-9_]*$')


def table_name(collection: str) -> str:
    '''Return the physical table name for a logical collection.
    Validates the collection name so we never interpolate user-controlled
    identifiers into SQL.'''
    if not _TABLE_RE.match(collection):
        raise ValueError(f"Invalid collection name: {collection!r}")
    return f'snooze_{collection}'


def table_ident(collection: str) -> sql.Identifier:
    return sql.Identifier(table_name(collection))


class SchemaCache:
    '''Per-backend memo of which collection tables we have already ensured
    in this process. Safe across threads.'''

    def __init__(self) -> None:
        self._known: Set[str] = set()
        self._lock = Lock()

    def is_known(self, collection: str) -> bool:
        with self._lock:
            return collection in self._known

    def mark_known(self, collection: str) -> None:
        with self._lock:
            self._known.add(collection)

    def forget(self, collection: str) -> None:
        with self._lock:
            self._known.discard(collection)

    def reset(self) -> None:
        with self._lock:
            self._known.clear()


def ensure_collection(conn: Connection, collection: str, cache: SchemaCache) -> None:
    '''Create the per-collection table and its baseline indexes if they
    don't exist yet. Idempotent and cheap on the hot path thanks to the
    in-memory cache.'''
    if cache.is_known(collection):
        return
    tbl = table_ident(collection)
    gin_idx = sql.Identifier(f'idx_{table_name(collection)}_data_gin')
    upd_idx = sql.Identifier(f'idx_{table_name(collection)}_updated_at')
    statements = [
        sql.SQL(
            'CREATE TABLE IF NOT EXISTS {} ('
            'uid TEXT PRIMARY KEY, '
            'data JSONB NOT NULL, '
            'updated_at TIMESTAMPTZ NOT NULL DEFAULT now())'
        ).format(tbl),
        sql.SQL('CREATE INDEX IF NOT EXISTS {} ON {} USING GIN (data jsonb_path_ops)').format(gin_idx, tbl),
        sql.SQL('CREATE INDEX IF NOT EXISTS {} ON {} (updated_at)').format(upd_idx, tbl),
    ]
    with conn.cursor() as cur:
        for stmt in statements:
            cur.execute(stmt)
    conn.commit()
    cache.mark_known(collection)
    log.debug("Ensured collection table %s", table_name(collection))


def list_collection_tables(conn: Connection) -> list[str]:
    '''Return the logical collection names backed by ``snooze_<name>``
    tables in the current schema search path.'''
    with conn.cursor() as cur:
        cur.execute(
            "SELECT tablename FROM pg_tables "
            "WHERE schemaname = ANY (current_schemas(false)) "
            "AND tablename LIKE 'snooze_%'"
        )
        rows = cur.fetchall()
    # Rows may be dicts (dict_row factory) or tuples depending on caller setup.
    out: list[str] = []
    for row in rows:
        if isinstance(row, dict):
            name = row['tablename']
        else:
            name = row[0]
        out.append(name[len('snooze_'):])
    return out
