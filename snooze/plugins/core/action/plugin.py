#!/usr/bin/python3.6


import json
from copy import deepcopy
from snooze.utils import Condition

from logging import getLogger
log = getLogger('snooze.notification')

from snooze.plugins.core import Plugin

class Action(Plugin):
    def reload_data(self, sync = False):
        super().reload_data()
        notification_plugin = self.core.get_core_plugin('notification')
        if notification_plugin:
            notification_plugin.reload_data()
        if sync and self.core.cluster:
            self.core.cluster.reload_plugin(self.name)
