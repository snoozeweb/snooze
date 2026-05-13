#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#
'''PostgreSQL implementation of the :class:`snooze.db.database.Database`
backend. Documents live in per-collection ``snooze_<name>`` tables backed
by a single ``jsonb`` column.'''

import datetime
import json
import os
import uuid
from copy import deepcopy
from logging import getLogger
from typing import Dict, List, Optional, Tuple, Union

import psycopg
from psycopg import sql
from psycopg.rows import dict_row
from psycopg.types.json import Jsonb

try:
    from psycopg_pool import ConnectionPool
except ImportError as exc:  # pragma: no cover
    raise ImportError(
        "The PostgreSQL backend requires the `pool` extra: "
        "install snooze-server with `psycopg[binary,pool]`."
    ) from exc

from snooze.db.database import Database
from snooze.db.postgres.convert import (
    convert as _convert,
    render_order_by,
    render_pagination,
)
from snooze.db.postgres.schema import (
    SchemaCache,
    ensure_collection,
    list_collection_tables,
    table_ident,
)
from snooze.utils.functions import dig

log = getLogger('snooze.db.postgres')


def _dsn_from_config(config) -> str:
    '''Build a libpq DSN from either an explicit ``dsn`` field or the
    decomposed host/port/database/user/password/sslmode fields. Missing
    pieces fall back to libpq's standard ``PG*`` environment variables.'''
    explicit = getattr(config, 'dsn', None)
    if explicit:
        return explicit
    parts = []
    for field, env_name in (
        ('host', 'PGHOST'),
        ('port', 'PGPORT'),
        ('database', 'PGDATABASE'),
        ('user', 'PGUSER'),
        ('password', 'PGPASSWORD'),
        ('sslmode', 'PGSSLMODE'),
    ):
        value = getattr(config, field, None) or os.environ.get(env_name)
        if value is not None:
            # libpq DSN quoting: wrap values containing spaces in single quotes.
            if isinstance(value, str) and ' ' in value:
                value = "'" + value.replace("'", r"\'") + "'"
            parts.append(f"{field if field != 'database' else 'dbname'}={value}")
    return ' '.join(parts) or ''


class BackendDB(Database):
    '''PostgreSQL backend. One :class:`ConnectionPool` is shared across
    threads; each operation acquires a connection for its duration.'''

    name = 'postgres'
    search_fields: Dict[str, List[str]]

    def __init__(self, config) -> None:
        dsn = _dsn_from_config(config)
        pool_kwargs = {
            'min_size': getattr(config, 'pool_min_size', 1),
            'max_size': getattr(config, 'pool_max_size', 10),
            'kwargs': {'row_factory': dict_row},
        }
        self.pool = ConnectionPool(dsn, **pool_kwargs)
        self.pool.wait(timeout=10.0)
        self.search_fields = {}
        self._schema = SchemaCache()
        log.debug('Initialized Postgres backend (dsn=%s)', dsn or '<from PG* env>')

    # ------------------------------------------------------------------ #
    # Helpers                                                            #
    # ------------------------------------------------------------------ #

    def _conn_ctx(self):
        return self.pool.connection()

    def _ensure(self, conn, collection: str) -> None:
        ensure_collection(conn, collection, self._schema)

    # ------------------------------------------------------------------ #
    # Indices / collections / convert                                    #
    # ------------------------------------------------------------------ #

    def create_index(self, collection: str, fields: List[str]) -> None:
        # Same semantics as the Mongo backend: this records the columns
        # that ``SEARCH`` should target when no explicit field list is
        # supplied. The GIN index over the JSONB column already covers
        # exact-match lookups; per-field btree indexes are a future
        # optimisation.
        log.debug("Register search fields for %s: %s", collection, fields)
        self.search_fields[collection] = fields

    def list_collections(self) -> List[str]:
        with self._conn_ctx() as conn:
            return list_collection_tables(conn)

    def drop(self, collection: str) -> None:
        with self._conn_ctx() as conn:
            with conn.cursor() as cur:
                cur.execute(sql.SQL('DROP TABLE IF EXISTS {}').format(table_ident(collection)))
            conn.commit()
        self._schema.forget(collection)

    def convert(self, condition, search_fields: List[str] = []):
        # Returned as a Composable; callers wire it under WHERE.
        return _convert(condition, search_fields)

    # ------------------------------------------------------------------ #
    # CRUD                                                               #
    # ------------------------------------------------------------------ #

    def write(
        self,
        collection: str,
        obj: Union[List[dict], dict],
        primary: Optional[str] = None,
        duplicate_policy: str = 'update',
        update_time: bool = True,
        constant: Optional[str] = None,
    ) -> dict:
        '''Port of the Mongo write() semantics. Branches on uid presence,
        primary-key lookups, duplicate policy, and constant-field guards.'''
        added: List[dict] = []
        rejected: List[dict] = []
        updated: List[dict] = []
        replaced: List[dict] = []
        to_insert: List[dict] = []

        tobjs = deepcopy(obj)
        if not isinstance(tobjs, list):
            tobjs = [tobjs]
        primary_keys = primary.split(',') if isinstance(primary, str) else (primary or None)
        constant_keys = constant.split(',') if isinstance(constant, str) else (constant or None)

        with self._conn_ctx() as conn:
            self._ensure(conn, collection)
            tbl = table_ident(collection)

            for tobj in tobjs:
                tobj.pop('_id', None)
                tobj.pop('_old', None)
                if update_time:
                    tobj['date_epoch'] = datetime.datetime.now().timestamp()
                old: dict = {}
                primary_result = None

                if primary_keys and all(dig(tobj, *p.split('.')) for p in primary_keys):
                    primary_cond = ['AND', *[['=', p, dig(tobj, *p.split('.'))] for p in primary_keys]] \
                        if len(primary_keys) > 1 else ['=', primary_keys[0], dig(tobj, *primary_keys[0].split('.'))]
                    primary_result = self._find_one(conn, collection, primary_cond)
                    if primary_result:
                        log.debug("Document with same primary %s: %s",
                                  primary_keys, primary_result.get('uid', ''))

                if 'uid' in tobj:
                    existing = self._find_one(conn, collection, ['=', 'uid', tobj['uid']])
                    if existing:
                        old = existing
                        if primary_result and primary_result.get('uid') != tobj['uid']:
                            msg = (f"Found another document with same primary {primary_keys}: "
                                   f"{primary_result}. Since UID is different, cannot update")
                            log.error(msg)
                            tobj['error'] = msg
                            rejected.append(tobj)
                        elif constant_keys and any(
                                existing.get(c, '') != tobj.get(c) for c in constant_keys):
                            msg = (f"Found a document with existing uid {tobj['uid']} but "
                                   f"different constant values: {constant_keys}. Cannot update")
                            log.error(msg)
                            tobj['error'] = msg
                            rejected.append(tobj)
                        elif duplicate_policy == 'replace':
                            self._replace_row(conn, tbl, tobj['uid'], tobj)
                            replaced.append(tobj)
                        else:
                            self._update_row(conn, tbl, tobj['uid'], tobj)
                            updated.append(tobj)
                    else:
                        msg = f"UID {tobj['uid']} not found. Skipping..."
                        log.error(msg)
                        tobj['error'] = msg
                        rejected.append(tobj)
                elif primary_keys:
                    if primary_result:
                        old = primary_result
                        if constant_keys and any(
                                primary_result.get(c, '') != tobj.get(c) for c in constant_keys):
                            msg = (f"Found a document with existing primary {primary_keys} but "
                                   f"different constant values: {constant_keys}.")
                            tobj['error'] = msg
                            rejected.append(tobj)
                        else:
                            if duplicate_policy == 'insert':
                                to_insert.append(self._with_uid(tobj))
                                added.append(tobj)
                            elif duplicate_policy == 'reject':
                                msg = f"Another object exists with the same {primary_keys}"
                                tobj['error'] = msg
                                rejected.append(tobj)
                            elif duplicate_policy == 'replace':
                                target_uid = primary_result.get('uid') or str(uuid.uuid4())
                                tobj['uid'] = target_uid
                                self._replace_row(conn, tbl, target_uid, tobj)
                                replaced.append(tobj)
                            else:
                                target_uid = primary_result.get('uid')
                                if target_uid:
                                    self._update_row(conn, tbl, target_uid, tobj)
                                else:
                                    to_insert.append(self._with_uid(tobj))
                                updated.append(tobj)
                    else:
                        to_insert.append(self._with_uid(tobj))
                        added.append(tobj)
                else:
                    to_insert.append(self._with_uid(tobj))
                    added.append(tobj)

                if old:
                    tobj['_old'] = old

            if to_insert:
                with conn.cursor() as cur:
                    cur.executemany(
                        sql.SQL('INSERT INTO {} (uid, data) VALUES (%s, %s)').format(tbl),
                        [(row['uid'], Jsonb(row)) for row in to_insert],
                    )
            conn.commit()
        return {'data': {'added': added, 'updated': updated,
                         'replaced': replaced, 'rejected': rejected}}

    @staticmethod
    def _with_uid(obj: dict) -> dict:
        if 'uid' not in obj:
            obj['uid'] = str(uuid.uuid4())
        return obj

    def _find_one(self, conn, collection: str, condition) -> Optional[dict]:
        '''Return the first ``data`` payload matching ``condition``, or None.'''
        self._ensure(conn, collection)
        tbl = table_ident(collection)
        where = _convert(condition, self.search_fields.get(collection, []))
        with conn.cursor() as cur:
            cur.execute(sql.SQL('SELECT data FROM {} WHERE {} LIMIT 1').format(tbl, where))
            row = cur.fetchone()
        return row['data'] if row else None

    def _replace_row(self, conn, tbl: sql.Identifier, uid: str, obj: dict) -> None:
        with conn.cursor() as cur:
            cur.execute(
                sql.SQL('INSERT INTO {} (uid, data) VALUES (%s, %s) '
                        'ON CONFLICT (uid) DO UPDATE SET data = EXCLUDED.data, '
                        'updated_at = now()').format(tbl),
                (uid, Jsonb(obj)),
            )

    def _update_row(self, conn, tbl: sql.Identifier, uid: str, obj: dict) -> None:
        with conn.cursor() as cur:
            cur.execute(
                sql.SQL('UPDATE {} SET data = data || %s::jsonb, updated_at = now() '
                        'WHERE uid = %s').format(tbl),
                (Jsonb(obj), uid),
            )

    def get_one(self, collection: str, search: dict):
        cond = self._search_dict_to_condition(search)
        with self._conn_ctx() as conn:
            return self._find_one(conn, collection, cond)

    @staticmethod
    def _search_dict_to_condition(search: dict):
        if not search:
            return None
        clauses = [['=', k, v] for k, v in search.items()]
        if len(clauses) == 1:
            return clauses[0]
        return ['AND', *clauses]

    def replace_one(self, collection: str, search: dict, obj: dict, update_time: bool = True) -> int:
        '''Replace the first document matching ``search`` with ``obj``; upsert
        if no match. Returns the number of pre-existing matches (0 or 1) so the
        caller can tell upsert from replace, matching the Mongo contract.'''
        new_obj = dict(obj)
        new_obj.pop('_id', None)
        for k, v in search.items():
            new_obj[k] = v
        if update_time:
            new_obj['date_epoch'] = datetime.datetime.now().timestamp()
        cond = self._search_dict_to_condition(search)
        with self._conn_ctx() as conn:
            self._ensure(conn, collection)
            tbl = table_ident(collection)
            where = _convert(cond, self.search_fields.get(collection, []))
            with conn.cursor() as cur:
                cur.execute(sql.SQL('SELECT uid FROM {} WHERE {} LIMIT 1').format(tbl, where))
                row = cur.fetchone()
            if row:
                target_uid = row['uid']
                new_obj.setdefault('uid', target_uid)
                self._replace_row(conn, tbl, target_uid, new_obj)
                matched = 1
            else:
                target_uid = new_obj.get('uid') or str(uuid.uuid4())
                new_obj['uid'] = target_uid
                with conn.cursor() as cur:
                    cur.execute(
                        sql.SQL('INSERT INTO {} (uid, data) VALUES (%s, %s)').format(tbl),
                        (target_uid, Jsonb(new_obj)),
                    )
                matched = 0
            conn.commit()
        return matched

    def update_one(self, collection: str, uid: str, obj: dict, update_time: bool = True) -> None:
        new_obj = dict(obj)
        new_obj.pop('_id', None)
        if update_time:
            new_obj['date_epoch'] = datetime.datetime.now().timestamp()
        # Always write the uid so an upsert from a fresh row stays addressable.
        new_obj.setdefault('uid', uid)
        with self._conn_ctx() as conn:
            self._ensure(conn, collection)
            tbl = table_ident(collection)
            with conn.cursor() as cur:
                cur.execute(
                    sql.SQL('INSERT INTO {} (uid, data) VALUES (%s, %s) '
                            'ON CONFLICT (uid) DO UPDATE SET '
                            'data = {}.data || EXCLUDED.data, updated_at = now()').format(tbl, tbl),
                    (uid, Jsonb(new_obj)),
                )
            conn.commit()

    # ------------------------------------------------------------------ #
    # Search / delete                                                    #
    # ------------------------------------------------------------------ #

    def search(self, collection: str, condition=None, **pagination):
        with self._conn_ctx() as conn:
            if collection not in self.list_collections():
                log.debug("search: collection %s does not exist", collection)
                return {'data': [], 'count': 0}
            tbl = table_ident(collection)
            where = _convert(condition, self.search_fields.get(collection, []))
            extra: List[sql.Composable] = []
            orderby = pagination.get('orderby')
            nb_per_page = int(pagination.get('nb_per_page', 0) or 0)
            # Accept both ``page_number`` (used by the falcon routes and
            # the Mongo backend) and ``page_nb`` (the name in the
            # ``Pagination`` TypedDict).
            page_nb = int(pagination.get('page_number') or pagination.get('page_nb') or 1)
            if orderby:
                extra.extend(render_order_by(orderby, asc=bool(pagination.get('asc', True))))
            elif nb_per_page > 0:
                # Without an explicit sort, default to insertion order via
                # the auto-incrementing ``seq`` column. Matches Mongo's
                # natural-order semantics and is stable across pages even
                # for rows inserted in the same transaction.
                extra.append(sql.SQL('ORDER BY seq ASC'))
            with conn.cursor() as cur:
                cur.execute(
                    sql.SQL('SELECT count(*) AS c FROM {} WHERE {}').format(tbl, where),
                )
                count_row = cur.fetchone()
                count = count_row['c'] if count_row else 0

                query = sql.SQL('SELECT data FROM {} WHERE {}').format(tbl, where)
                if extra:
                    query = sql.SQL(' ').join([query, *extra])
                if nb_per_page > 0:
                    query = sql.SQL(' ').join([query, *render_pagination(nb_per_page, page_nb)])
                cur.execute(query)
                rows = cur.fetchall()
            return {'data': [r['data'] for r in rows], 'count': count}

    def delete(self, collection: str, condition=None, force: bool = False) -> dict:
        with self._conn_ctx() as conn:
            if collection not in self.list_collections():
                log.debug("delete: collection %s does not exist", collection)
                return {'data': [], 'count': 0}
            tbl = table_ident(collection)
            if not condition and not force:
                log.warning("delete called with empty condition and force=False; refusing")
                return {'data': [], 'count': 0}
            where = _convert(condition, self.search_fields.get(collection, []))
            with conn.cursor() as cur:
                cur.execute(
                    sql.SQL('DELETE FROM {} WHERE {} RETURNING data').format(tbl, where),
                )
                rows = cur.fetchall()
            conn.commit()
            data = [r['data'] for r in rows]
            return {'data': data, 'count': len(data)}

    # ------------------------------------------------------------------ #
    # Increments / list ops                                              #
    # ------------------------------------------------------------------ #

    def bulk_increment(self, collection: str, updates: List[Tuple[dict, dict]],
                       upsert: bool = False) -> None:
        '''Mongo: $inc matched rows; with upsert, insert (search ∪ update).
        Postgres: find matching row by the (arbitrary) search dict, then either
        UPDATE its counter fields or INSERT a new row. Wrapped in a single
        transaction so the batch is atomic.'''
        if not updates:
            return
        with self._conn_ctx() as conn:
            self._ensure(conn, collection)
            tbl = table_ident(collection)
            with conn.cursor() as cur:
                for search, delta in updates:
                    cond = self._search_dict_to_condition(search)
                    where = _convert(cond, self.search_fields.get(collection, []))
                    cur.execute(
                        sql.SQL('SELECT uid, data FROM {} WHERE {} LIMIT 1').format(tbl, where),
                    )
                    row = cur.fetchone()
                    if row:
                        data = row['data']
                        for field, value in delta.items():
                            data[field] = (data.get(field) or 0) + value
                        cur.execute(
                            sql.SQL('UPDATE {} SET data = %s::jsonb, updated_at = now() '
                                    'WHERE uid = %s').format(tbl),
                            (Jsonb(data), row['uid']),
                        )
                    elif upsert:
                        new_obj = {**self._sanitise_for_jsonb(search), **delta}
                        new_obj.setdefault('uid', str(uuid.uuid4()))
                        cur.execute(
                            sql.SQL('INSERT INTO {} (uid, data) VALUES (%s, %s)').format(tbl),
                            (new_obj['uid'], Jsonb(new_obj)),
                        )
            conn.commit()

    @staticmethod
    def _sanitise_for_jsonb(payload: dict) -> dict:
        '''Convert non-JSON-native types (datetime, etc.) so jsonb storage
        keeps the same shape as the Mongo backend.'''
        out: dict = {}
        for k, v in payload.items():
            if isinstance(v, datetime.datetime):
                out[k] = v.timestamp()
            else:
                out[k] = v
        return out

    def inc_many(self, collection: str, field: str, condition=None, value: int = 1) -> None:
        with self._conn_ctx() as conn:
            self._ensure(conn, collection)
            tbl = table_ident(collection)
            where = _convert(condition, self.search_fields.get(collection, []))
            with conn.cursor() as cur:
                # COALESCE handles missing keys (treat as 0). The expression
                # both reads and writes the same JSONB key in one statement.
                cur.execute(
                    sql.SQL(
                        'UPDATE {tbl} SET data = jsonb_set('
                        'data, {path}, '
                        "to_jsonb((COALESCE((data->>{f})::numeric, 0) + {v})), true), "
                        'updated_at = now() WHERE {where}'
                    ).format(
                        tbl=tbl,
                        path=sql.Literal('{' + field + '}'),
                        f=sql.Literal(field),
                        v=sql.Literal(value),
                        where=where,
                    ),
                )
            conn.commit()

    def _update_fields_via_python(self, collection: str, mutator, condition) -> None:
        '''Helper for set/append/prepend/remove: load each matching row,
        let ``mutator`` rewrite its data dict in place, and write it back.
        Not the fastest path but it keeps semantics aligned with the Mongo
        backend without rebuilding every Mongo operator in SQL.'''
        with self._conn_ctx() as conn:
            self._ensure(conn, collection)
            tbl = table_ident(collection)
            where = _convert(condition, self.search_fields.get(collection, []))
            with conn.cursor() as cur:
                cur.execute(
                    sql.SQL('SELECT uid, data FROM {} WHERE {}').format(tbl, where),
                )
                rows = cur.fetchall()
                for row in rows:
                    data = row['data']
                    mutator(data)
                    cur.execute(
                        sql.SQL('UPDATE {} SET data = %s::jsonb, updated_at = now() '
                                'WHERE uid = %s').format(tbl),
                        (Jsonb(data), row['uid']),
                    )
            conn.commit()

    def set_fields(self, collection: str, fields: dict, condition=None) -> None:
        def mutate(data: dict) -> None:
            for k, v in fields.items():
                data[k] = v
        self._update_fields_via_python(collection, mutate, condition)

    def append_list(self, collection: str, fields: dict, condition=None) -> None:
        def mutate(data: dict) -> None:
            for k, values in fields.items():
                existing = data.get(k) or []
                if not isinstance(existing, list):
                    existing = [existing]
                data[k] = [*existing, *values]
        self._update_fields_via_python(collection, mutate, condition)

    def prepend_list(self, collection: str, fields: dict, condition=None) -> None:
        def mutate(data: dict) -> None:
            for k, values in fields.items():
                existing = data.get(k) or []
                if not isinstance(existing, list):
                    existing = [existing]
                data[k] = [*values, *existing]
        self._update_fields_via_python(collection, mutate, condition)

    def remove_list(self, collection: str, fields: dict, condition=None) -> None:
        def mutate(data: dict) -> None:
            for k, values in fields.items():
                existing = data.get(k) or []
                if not isinstance(existing, list):
                    existing = [existing]
                drop = set(values) if all(isinstance(v, (str, int, float)) for v in values) else None
                if drop is not None:
                    data[k] = [item for item in existing if item not in drop]
                else:
                    data[k] = [item for item in existing if item not in values]
        self._update_fields_via_python(collection, mutate, condition)

    # ------------------------------------------------------------------ #
    # Stats & maintenance                                                #
    # ------------------------------------------------------------------ #

    def compute_stats(self, collection: str, date_from, date_until, groupby: str = 'hour'):
        '''Recreate the Mongo aggregation pipeline against the JSONB store.
        Records carry ``date`` (timestamptz-ish) and ``key`` / ``value`` fields.
        Output shape mirrors the Mongo backend so callers don't branch.'''
        date_from = date_from.replace(minute=0, second=0, microsecond=0)
        log.debug("Compute metrics on `%s` from %s until %s grouped by %s",
                  collection, date_from, date_until, groupby)
        if collection not in self.list_collections():
            log.debug("compute_stats: collection %s does not exist", collection)
            return {'data': [], 'count': 0}

        trunc_unit = {
            'hour': 'hour', 'day': 'day', 'month': 'month',
            'year': 'year', 'week': 'week', 'weekday': 'dow',
        }.get(groupby, 'hour')
        tbl = table_ident(collection)

        with self._conn_ctx() as conn, conn.cursor() as cur:
            cur.execute(
                sql.SQL(
                    'WITH src AS ('
                    "  SELECT (data->>'date')::timestamptz AS d, "
                    "         data->>'key' AS k, "
                    "         COALESCE((data->>'value')::numeric, 0) AS v "
                    '  FROM {tbl} '
                    "  WHERE (data->>'date')::timestamptz BETWEEN %s AND %s "
                    ') '
                    'SELECT to_char(date_trunc(%s, d), \'YYYY-MM-DD"T"HH24:MI:OF\') AS bucket, '
                    'k AS key, SUM(v) AS value '
                    'FROM src '
                    'GROUP BY bucket, k '
                    'ORDER BY bucket'
                ).format(tbl=tbl),
                (date_from, date_until, trunc_unit),
            )
            rows = cur.fetchall()

        grouped: Dict[str, List[dict]] = {}
        for row in rows:
            grouped.setdefault(row['bucket'], []).append(
                {'key': row['key'], 'value': float(row['value'])},
            )
        result = [{'_id': bucket, 'data': entries} for bucket, entries in grouped.items()]
        return {'data': result, 'count': len(result)}

    def cleanup_timeout(self, collection: str) -> int:
        '''Delete records whose ``date_epoch + ttl`` has elapsed.'''
        if collection not in self.list_collections():
            return 0
        tbl = table_ident(collection)
        with self._conn_ctx() as conn, conn.cursor() as cur:
            cur.execute(
                sql.SQL(
                    'DELETE FROM {} WHERE '
                    "(data->>'ttl')::numeric >= 0 AND "
                    "(COALESCE((data->>'date_epoch')::numeric, 0) + "
                    " COALESCE((data->>'ttl')::numeric, 0)) <= extract(epoch from now())"
                ).format(tbl),
            )
            deleted = cur.rowcount or 0
            conn.commit()
        return deleted

    def cleanup_comments(self) -> int:
        '''Drop comments whose ``record_uid`` no longer resolves.'''
        existing = self.list_collections()
        if 'comment' not in existing or 'record' not in existing:
            return 0
        with self._conn_ctx() as conn, conn.cursor() as cur:
            cur.execute(
                sql.SQL(
                    "DELETE FROM {ct} WHERE data->>'record_uid' NOT IN "
                    "(SELECT data->>'uid' FROM {rt} WHERE data ? 'uid')"
                ).format(ct=table_ident('comment'), rt=table_ident('record')),
            )
            deleted = cur.rowcount or 0
            conn.commit()
        return deleted

    def cleanup_orphans(self, collection: str) -> int:
        '''Drop documents whose ``parents`` array references a non-existent
        ancestor in the same collection.'''
        if collection not in self.list_collections():
            return 0
        tbl = table_ident(collection)
        with self._conn_ctx() as conn, conn.cursor() as cur:
            cur.execute(
                sql.SQL(
                    'WITH parents AS ('
                    "  SELECT DISTINCT (data->'parents'->-1) #>> '{{}}' AS parent "
                    '  FROM {tbl} '
                    "  WHERE jsonb_typeof(data->'parents') = 'array' "
                    "  AND jsonb_array_length(data->'parents') > 0 "
                    '), '
                    'missing AS ('
                    '  SELECT parent FROM parents WHERE parent IS NOT NULL AND parent NOT IN ('
                    "    SELECT data->>'uid' FROM {tbl} WHERE data ? 'uid'"
                    '  )'
                    ') '
                    'DELETE FROM {tbl} WHERE EXISTS ('
                    "  SELECT 1 FROM jsonb_array_elements_text(data->'parents') p, missing m "
                    '  WHERE p = m.parent'
                    ')'
                ).format(tbl=tbl),
            )
            deleted = cur.rowcount or 0
            conn.commit()
        return deleted

    def cleanup_audit_logs(self, interval: float) -> None:
        '''Delete audit rows for objects last marked deleted before
        ``now - interval``. The Mongo version pages over the most recent
        event per object_id; in SQL we use DISTINCT ON.'''
        if 'audit' not in self.list_collections():
            return
        threshold = datetime.datetime.now().astimezone().timestamp() - interval
        log.debug("Audit cleanup threshold: %s",
                  datetime.datetime.fromtimestamp(threshold).astimezone())
        with self._conn_ctx() as conn, conn.cursor() as cur:
            cur.execute(
                sql.SQL(
                    'WITH latest AS ('
                    "  SELECT DISTINCT ON (data->>'object_id') "
                    "    data->>'object_id' AS oid, "
                    "    data->>'action' AS action, "
                    "    COALESCE((data->>'date_epoch')::numeric, 0) AS de "
                    '  FROM {tbl} '
                    "  ORDER BY data->>'object_id', (data->>'timestamp')::timestamptz DESC NULLS LAST "
                    ') '
                    'DELETE FROM {tbl} WHERE data ? \'object_id\' AND '
                    "data->>'object_id' IN ("
                    "  SELECT oid FROM latest WHERE action = 'deleted' AND de < %s"
                    ')'
                ).format(tbl=table_ident('audit')),
                (threshold,),
            )
            conn.commit()

    def renumber_field(self, collection: str, field: str) -> None:
        '''Re-pack the positional ``field`` so values are contiguous from 0.'''
        if collection not in self.list_collections():
            return
        tbl = table_ident(collection)
        with self._conn_ctx() as conn, conn.cursor() as cur:
            cur.execute(
                sql.SQL(
                    'WITH ranked AS ('
                    '  SELECT uid, row_number() OVER ('
                    '    ORDER BY (data->>{fld})::numeric ASC NULLS LAST, uid ASC'
                    '  ) - 1 AS new_pos '
                    '  FROM {tbl}'
                    ') '
                    'UPDATE {tbl} t SET '
                    'data = jsonb_set(t.data, {path}, to_jsonb(r.new_pos), true), '
                    'updated_at = now() '
                    'FROM ranked r WHERE r.uid = t.uid'
                ).format(
                    tbl=tbl,
                    fld=sql.Literal(field),
                    path=sql.Literal('{' + field + '}'),
                ),
            )
            conn.commit()

    # ------------------------------------------------------------------ #
    # Backup                                                             #
    # ------------------------------------------------------------------ #

    def backup(self, backup_path: str, backup_exclude: Optional[List[str]] = None) -> None:
        exclude = set(backup_exclude or [])
        os.makedirs(backup_path, exist_ok=True)
        for collection in self.list_collections():
            if collection in exclude:
                continue
            with self._conn_ctx() as conn, conn.cursor() as cur:
                cur.execute(sql.SQL('SELECT data FROM {}').format(table_ident(collection)))
                rows = cur.fetchall()
            target = os.path.join(backup_path, f'{collection}.json')
            with open(target, 'w', encoding='utf-8') as fh:
                json.dump([r['data'] for r in rows], fh, default=str)
            log.debug("Backed up %d rows from %s to %s", len(rows), collection, target)

    # ------------------------------------------------------------------ #
    # Lifecycle                                                          #
    # ------------------------------------------------------------------ #

    def close(self) -> None:
        try:
            self.pool.close()
        except Exception:  # pragma: no cover
            log.exception("Error closing Postgres pool")
