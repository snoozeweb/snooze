#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

#!/usr/bin/python3.6


import json
from snooze.utils import get_condition

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
