#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''Plugin definition for the KV'''

from logging import getLogger

from snooze.plugins.core import Plugin

log = getLogger('snooze.kv')

class Kv(Plugin):
    '''Plugin for managing a user/script defined key-value in the database'''
    def reload_data(self, sync = False):
        super().reload_data()
        kv = {}
        for key_val in self.data:
            try:
                if key_val['dict'] not in kv:
                    kv[key_val['dict']] = {}
                kv[key_val['dict']][key_val['key']] = key_val['value']
            except Exception as err:
                log.exception(err)
                continue
        self.kv = kv
        if sync:
            self.sync_neighbors()

    def get(self, mydict, key):
        '''Return the value for a given dictionary and key'''
        return self.kv[mydict][key]
