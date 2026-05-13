#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#
'''Condition DSL → SQL WHERE clause translator for the Postgres backend.

Mirrors ``snooze.db.mongo.database.BackendDB.convert`` but emits
``psycopg.sql.Composable`` fragments referencing a single ``data jsonb``
column. Numeric and boolean operands are detected and cast accordingly so
the natural Postgres operators apply.'''

from typing import Iterable, Tuple

from psycopg import sql

from snooze.utils.condition import OperationNotSupported, unsugar_regex

SCALARS = (str, int, float, bool)


def _path_key(part: str):
    '''Path components that look like integers index into arrays
    (``a.1`` -> ``data->'a'->1``); everything else is an object key.'''
    if part.lstrip('-').isdigit():
        return sql.Literal(int(part))
    return sql.Literal(part)


def _field_path(field: str) -> sql.Composable:
    '''Translate a dotted field path into the JSONB navigation expression.
    ``"host"`` -> ``data->>'host'``; ``"a.b.c"`` -> ``data->'a'->'b'->>'c'``.
    Numeric path components are emitted as integer indices so array
    elements can be reached (``"a.1"`` -> ``data->'a'->>1``).
    The terminal arrow is ``->>`` so the result is text.'''
    parts = field.split('.')
    expr: sql.Composable = sql.SQL('data')
    for part in parts[:-1]:
        expr = sql.SQL('{}->{}').format(expr, _path_key(part))
    expr = sql.SQL('{}->>{}').format(expr, _path_key(parts[-1]))
    return expr


def _field_jsonb(field: str) -> sql.Composable:
    '''Same as :func:`_field_path` but returning JSONB (final ``->``).'''
    parts = field.split('.')
    expr: sql.Composable = sql.SQL('data')
    for part in parts:
        expr = sql.SQL('{}->{}').format(expr, _path_key(part))
    return expr


def _is_numeric(value) -> bool:
    return isinstance(value, (int, float)) and not isinstance(value, bool)


def _typed_compare(field: str, op: str, value) -> sql.Composable:
    '''Render ``field OP value`` with the right cast for the operand type.
    JSONB ``->>`` returns text, so for numeric comparisons we cast to numeric.
    For bool we compare against the lower-cased text representation.
    The field expression is wrapped in an extra pair of parens because
    ``data->>'k'::numeric`` parses as ``data->>('k'::numeric)``.'''
    op_sql = sql.SQL(op)
    # Booleans must be checked first because bool is a subclass of int.
    if isinstance(value, bool):
        return sql.SQL('({} {} {})').format(
            _field_path(field), op_sql, sql.Literal('true' if value else 'false'),
        )
    if _is_numeric(value):
        return sql.SQL('(({})::numeric {} {})').format(
            _field_path(field), op_sql, sql.Literal(value),
        )
    return sql.SQL('({} {} {})').format(
        _field_path(field), op_sql, sql.Literal(value),
    )


def convert(condition, search_fields: Iterable[str] = ()) -> sql.Composable:
    '''Translate a condition AST to a ``psycopg.sql.Composable`` boolean
    expression suitable for use after ``WHERE``. Returns ``TRUE`` when the
    condition is empty (matches Mongo's ``{}`` semantics).'''
    if not condition:
        return sql.SQL('TRUE')
    operation, *args = condition

    if operation == 'AND':
        if not args:
            return sql.SQL('TRUE')
        parts = [convert(arg, search_fields) for arg in args]
        return sql.SQL('({})').format(sql.SQL(' AND ').join(parts))

    if operation == 'OR':
        if not args:
            return sql.SQL('FALSE')
        parts = [convert(arg, search_fields) for arg in args]
        return sql.SQL('({})').format(sql.SQL(' OR ').join(parts))

    if operation == 'NOT':
        return sql.SQL('(NOT {})').format(convert(args[0], search_fields))

    if operation == '=':
        key, value = args
        if value is None:
            return sql.SQL('({} IS NULL OR NOT (data ? {}))').format(
                _field_path(key), sql.Literal(key.split('.')[0]),
            )
        return _typed_compare(key, '=', value)

    if operation == '!=':
        key, value = args
        if value is None:
            return sql.SQL('({} IS NOT NULL)').format(_field_path(key))
        return sql.SQL('({} IS DISTINCT FROM {})').format(
            sql.SQL('({})::numeric').format(_field_path(key))
            if _is_numeric(value) else _field_path(key),
            sql.Literal(value if not isinstance(value, bool)
                        else ('true' if value else 'false')),
        )

    if operation in ('>', '>=', '<', '<='):
        key, value = args
        return _typed_compare(key, operation, value)

    if operation == 'MATCHES':
        key, value = args
        pattern = unsugar_regex(str(value))
        return sql.SQL('({} ~* {})').format(_field_path(key), sql.Literal(pattern))

    if operation == 'EXISTS':
        # Translate the dotted path to a json_path existence check so we
        # support nested fields. Top-level: data ? 'field' is faster.
        field = args[0]
        if '.' not in field:
            return sql.SQL('(data ? {})').format(sql.Literal(field))
        # For nested: the final segment is reached if every intermediate
        # jsonb_typeof yields object AND the leaf jsonb is not null.
        return sql.SQL('({} IS NOT NULL)').format(_field_jsonb(field))

    if operation == 'CONTAINS':
        key, value = args
        values = value if isinstance(value, list) else [value]
        # CONTAINS is "any element of the (flattened) field matches any of
        # the (flattened) value regexes, case-insensitive". Field may be a
        # scalar or a JSON array; handle both with a union of two
        # branches (Postgres rejects set-returning functions in CASE, so
        # we can't put ``jsonb_array_elements_text`` there).
        patterns = [unsugar_regex(str(v)) if isinstance(v, str) else str(v)
                    for v in values]
        if not patterns:
            return sql.SQL('FALSE')
        return sql.SQL(
            'COALESCE('
            '({tx} ~* ANY({pats}::text[])) OR '
            "(jsonb_typeof({jb}) = 'array' AND EXISTS ("
            '  SELECT 1 FROM jsonb_array_elements_text({jb}) v '
            '  WHERE v ~* ANY({pats}::text[])'
            ')), false)'
        ).format(
            tx=_field_path(key),
            jb=_field_jsonb(key),
            pats=sql.Literal(patterns),
        )

    if operation == 'IN':
        # Note: DSL arg order is ['IN', value_or_condition, field]
        value, key = args
        # If value looks like a nested condition (first element is a known
        # operator), evaluate it against each element of the field array.
        # ``AS arr(data)`` renames the per-row column to ``data`` so the
        # recursive convert() output (which references ``data->>...``)
        # binds to the array element.
        if isinstance(value, list) and value and isinstance(value[0], str) \
                and value[0] in _DSL_OPERATORS:
            inner = convert(value, search_fields)
            return sql.SQL(
                "COALESCE((jsonb_typeof({jb}) = 'array' AND EXISTS (SELECT 1 "
                'FROM jsonb_array_elements({jb}) AS arr(data) WHERE {pred})), '
                'false)'
            ).format(jb=_field_jsonb(key), pred=inner)
        # Literal list membership. The field may be either a scalar or a
        # JSONB array of scalars (e.g. ``parents``); match either shape
        # to keep parity with Mongo's ``$in`` semantics. ``COALESCE(...,
        # false)`` collapses missing-field NULLs to false so a downstream
        # ``NOT`` doesn't get NULL-propagated and silently drop the row.
        # Comparisons are text-based because JSONB ``->>`` only yields
        # text — a numeric cast on the whole-array text representation
        # (``["a", "b"]``) would explode for array-typed fields.
        items = value if isinstance(value, list) else [value]
        arr = sql.Literal([str(i) for i in items])
        return sql.SQL(
            'COALESCE('
            '{fld} = ANY({arr}::text[]) OR '
            "(jsonb_typeof({jb}) = 'array' AND EXISTS ("
            '  SELECT 1 FROM jsonb_array_elements_text({jb}) v '
            '  WHERE v = ANY({arr}::text[])'
            ')), false)'
        ).format(fld=_field_path(key), jb=_field_jsonb(key), arr=arr)

    if operation == 'SEARCH':
        needle = str(args[0])
        if search_fields:
            parts = [
                sql.SQL('({} ~* {})').format(_field_path(f), sql.Literal(needle))
                for f in search_fields
            ]
            return sql.SQL('({})').format(sql.SQL(' OR ').join(parts))
        # Without explicit fields, search the entire serialised document.
        return sql.SQL('(data::text ~* {})').format(sql.Literal(needle))

    raise OperationNotSupported(operation)


_DSL_OPERATORS = frozenset([
    'AND', 'OR', 'NOT',
    '=', '!=', '>', '>=', '<', '<=',
    'MATCHES', 'EXISTS', 'CONTAINS', 'IN', 'SEARCH',
])


def render_order_by(orderby: str, asc: bool = True) -> Tuple[sql.Composable, ...]:
    '''Render an ORDER BY clause for a dotted field path.

    JSONB ``->>`` always yields text, which would sort numeric fields
    lexicographically (``'10' < '2'``). To match the natural ordering
    callers expect, we emit a two-level ORDER BY: numeric-looking values
    sort first by their numeric value, everything else falls through to
    a stable text sort.'''
    direction = sql.SQL('ASC') if asc else sql.SQL('DESC')
    field = _field_path(orderby)
    return (
        sql.SQL(
            "ORDER BY "
            "CASE WHEN {fld} ~ '^-?[0-9]+(\\.[0-9]+)?$' "
            "THEN ({fld})::numeric END {dir} NULLS LAST, "
            "{fld} {dir} NULLS LAST"
        ).format(fld=field, dir=direction),
    )


def render_pagination(nb_per_page: int, page_nb: int) -> Tuple[sql.Composable, ...]:
    '''Render LIMIT/OFFSET. ``page_nb`` is 1-indexed (matches Mongo backend).'''
    page_nb = max(page_nb, 1)
    nb_per_page = max(nb_per_page, 1)
    return (
        sql.SQL('LIMIT {} OFFSET {}').format(
            sql.Literal(nb_per_page),
            sql.Literal((page_nb - 1) * nb_per_page),
        ),
    )


__all__ = ['convert', 'render_order_by', 'render_pagination']
