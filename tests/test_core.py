#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

#!/usr/bin/python3.6

import pytest
from snooze.core import Core
from snooze.__main__ import main
from pathlib import Path
from os import remove
import os

from logging import getLogger
log = getLogger('snooze.tests')

import mongomock

import yaml

class TestCore():
    def test_load_plugins(self, core):
        plugin_list = list(map(lambda x: x.name, core.plugins))
        assert all(plugin in plugin_list for plugin in ['record', 'rule', 'aggregaterule', 'snooze', 'notification'])
    def test_process_record(self, core):
        record = {'a': '1', 'b': '2'}
        core.process_record(record)
        search = core.db.search('record', ['AND', ['=', 'a', '1'], ['=', 'b', '2']])
        assert all(plugin in search['data'][0]['plugins'] for plugin in ['rule', 'aggregaterule', 'snooze', 'notification'])
    def test_process_ok(self, core):
        core.ok_severities = ['ok']
        record = {'severity': 'ok'}
        core.process_record(record)
        data = core.db.search('record')['data'][0]
        assert data['state'] == 'close'
