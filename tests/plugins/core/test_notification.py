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
    def notification(self, core, config):
        actions = [
            {'name': 'Script', 'action': {'selected': 'script', 'subcontent': {'script': '/bin/echo', 'arguments': ['test']}}}
        ]
        core.db.write('action', actions)
        notifications = [
            {'name': 'Notification1', 'condition': ['=', 'a', '1'], 'actions': ['Script']},
        ]
        core.db.write('notification', notifications)
        return Notification(core, config)
    def test_notification_echo(self, notification, record):
        notification.process(record)
    
class TestNotificationObject:
    def test_match_true(self, core):
        record = {'timestamp': '2021-07-01T12:00:00+09:00', 'host': 'myhost01', 'message': 'my message'}
        notification = {
            'name': 'Notification 1',
            'condition': ['=', 'host', 'myhost01'],
            'time_constraint': [
                {'type': 'Weekdays', 'content': {'weekdays': [1,2,3,4]}},
                {'type': 'Time', 'content': {'from': '10:00', 'until': '14:00'}}
            ],
        }
        assert NotificationObject(notification, core).match(record) == True
