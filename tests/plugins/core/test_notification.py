#!/usr/bin/python36

import pytest

from snooze.plugins.core.notification.plugin import Notification
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
            {'name': 'Notification1', 'condition': ['=', 'a', '1'], 'action': 'Script'},
        ]
        core.db.write('notification', notifications)
        return Notification(core, config)
    def test_notification_echo(self, notification, record):
        notification.process(record)
