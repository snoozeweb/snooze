#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#
'''PostgreSQL backend for Snooze.

Documents are stored one-table-per-collection in a single ``jsonb`` column,
preserving the schemaless contract the plugin system relies on. Table and
index creation happens lazily on first write to a collection.'''
