#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

import os
import re
import threading
import time

import pytest
from click.testing import CliRunner

from snooze.cli.__main__ import snooze
from snooze.api.socket import WSGISocketServer, admin_api
from snooze.token import TokenEngine

@pytest.fixture(scope='class')
def mysocket():
    token_engine = TokenEngine('secret')
    api = admin_api(token_engine)
    thread = WSGISocketServer(api, './test_root_token.socket')
    thread.daemon = True
    thread.start()
    time.sleep(0.1)
    return thread

def test_root_token(mysocket):
    path = os.path.abspath('./test_root_token.socket')
    runner = CliRunner()
    result = runner.invoke(snooze, ['root-token', '--socket', path])
    assert result.exit_code == 0
    assert re.match('Root token: .*', result.output)
