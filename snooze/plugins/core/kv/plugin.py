#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

#!/usr/bin/python3.6

from snooze.plugins.core import Plugin
import logging
from logging import getLogger
log = getLogger('snooze.kv')

class Kv(Plugin):
    def reload_data(self, sync = False):
        super().reload_data()
        self.kv = {}
        for key_val in self.data:
            try:
                if key_val['dict'] not in self.kv:
                    self.kv[key_val['dict']] = {}
                self.kv[key_val['dict']][key_val['key']] = key_val['value']
            except Exception as e:
                log.exception(e)
                continue
        if sync and self.core.cluster:
            self.core.cluster.reload_plugin(self.name)

    def get(self, mydict, key):
        return self.kv[mydict][key]
