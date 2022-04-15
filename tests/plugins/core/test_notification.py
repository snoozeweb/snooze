#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

#!/usr/bin/python36

import pytest

from snooze.plugins.core.notification.plugin import Notification, NotificationObject
from snooze.plugins.core import Abort, Abort_and_write

import pytest

class TestNotification:
    @pytest.fixture
    def record(self):
        return {'a': '1', 'b': '2'}
    @pytest.fixture
    def notification(self, core):
        actions = [
            {'name': 'Script', 'action': {'selected': 'script', 'subcontent': {'script': '/bin/echo', 'arguments': ['test']}}}
        ]
        core.db.write('action', actions)
        action = core.get_core_plugin('action')
        notifications = [
            {'name': 'Notification1', 'condition': ['=', 'a', '1'], 'actions': ['Script']},
        ]
        core.db.write('notification', notifications)
        notif = core.get_core_plugin('notification')
        return notif
    def test_notification_echo(self, notification, record):
        notification.process(record)

    def test_match_true(self, notification):
        record = {'timestamp': '2021-07-01T12:00:00+09:00', 'host': 'myhost01', 'message': 'my message'}
        notif_obj = {
            'name': 'Notification 1',
            'condition': ['=', 'host', 'myhost01'],
            'time_constraint': [
                {'type': 'Weekdays', 'content': {'weekdays': [1,2,3,4]}},
                {'type': 'Time', 'content': {'from': '10:00', 'until': '14:00'}}
            ],
        }
        assert NotificationObject(notif_obj, notification).match(record) == True
