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
        log.debug(search)
        assert all(plugin in search['data'][0]['plugins'] for plugin in ['rule', 'aggregaterule', 'snooze', 'notification'])
