#
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''
The syncer thread monitor the database for changes, and pull the object list
to the memory when a change happens. This allow snooze to keep processing fast
(since all values are in memory), and because changes are infrequent, and
human-triggered, the additional latency (1 second) is deemed acceptable.
'''

from collections import defaultdict
from time import sleep
from threading import Event
from typing import Optional, Dict
from logging import getLogger
from uuid import uuid4

from snooze.utils.threading import SurvivingThread

log = getLogger('snooze.api.syncer')

class Syncer(SurvivingThread):
    '''The syncer thread'''
    def __init__(self, core: 'Core', exit_event: Optional[Event] = None):
        self.config = core.config.syncer
        self.core = core
        self.database = core.db
        self.plugins = {p.name: p for p in core.plugins}
        # Status format:
        self.current: Dict[str, Dict[str, float]] = defaultdict(dict)

        SurvivingThread.__init__(self, exit_event)

    def start_thread(self):
        log.info('Start Syncer')
        while not self.exit.wait(0.1):
            self.poll()
            sleep(self.config.sync_interval_ms/1000)
        log.info('Stopped Syncer')

    def poll(self):
        '''Pull the status and sync with in-mem status if necessary'''
        latest = defaultdict(dict)

        for entry in self.database.search('syncer_latest')['data']:
            mytype = entry['type']
            name = entry['name']
            latest[mytype][name] = entry['timestamp']

        self.update_plugins(latest)
        self.update_config(latest)

    def get_status(self):
        '''Return the sync status. Used for API endpoint and monitoring script'''
        data = {
            'plugin': defaultdict(dict),
            'config': defaultdict(dict),
        }

        for e in self.database.search('syncer_latest')['data']:
            if e['type'] not in data:
                continue # Ignore unknown type
            if 'timestamp' in data[e['type']][e['name']]:
                data[e['type']][e['name']]['timestamp'] = e['timestamp']
            if 'node' in data[e['type']][e['name']]:
                data[e['type']][e['name']]['node'] = e['node']
        for e in self.database.search('syncer_node')['data']:
            if e['type'] not in data:
                continue # Ignore unknown type
            data[e['type']][e['name']].setdefault('total', 0)
            data[e['type']][e['name']]['total'] += 1
            data[e['type']][e['name']].setdefault('synced', 0)
            if e['timestamp'] >= data[e['type']][e['name']].get('timestamp', 0):
                data[e['type']][e['name']]['synced'] += 1
        return data

    def update_plugins(self, latest):
        '''Update outdated plugin in-memory data if any'''
        for name, plugin in self.plugins.items():
            if name not in latest['plugin']:
                # Add the plugin timestamp to latest if absent
                self.database.replace_one('syncer_latest', dict(type='plugin', name=name), {
                    'node': self.config.hostname,
                    'uid': str(uuid4()),
                    'type': 'plugin',
                    'name': name,
                    'timestamp': self.current['plugin'].get(name, 0)
                })
                continue
            if latest['plugin'][name] > self.current['plugin'].get(name, 0):
                log.info("Plugin '%s' data out-of-date", name)
                plugin.reload_data()
                self.database.replace_one('syncer_node', dict(node=self.config.hostname, type='plugin', name=name), {
                    'node': self.config.hostname,
                    'type': 'plugin',
                    'name': name,
                    'timestamp': latest['plugin'][name],
                })
                self.current['plugin'][name] = latest['plugin'][name]
                log.debug("Updated plugin '%s' status", name)

    def update_config(self, latest):
        '''Update outdated config in-memory data if any'''
        for section, config in self.core.config:
            if section not in latest['config']:
                # Add the plugin timestamp to latest if absent
                self.database.replace_one('syncer_latest', dict(type='config', name=section), {
                    'type': 'config',
                    'name': section,
                    'timestamp': self.current['config'].get(section, 0)
                })
                continue
            if latest['config'][section] > self.current['config'].get(section, 0):
                data = self.database.get_one('config', dict(section=section)) or {}
                config.update(data['config'])
                for auth in config.auth_routes():
                    auth_route = self.core.api.auth_routes.get(auth)
                    if auth_route:
                        auth_route.reload()
                self.database.replace_one('syncer_node', dict(node=self.config.hostname, type='config', name=section), {
                    'node': self.config.hostname,
                    'type': 'config',
                    'name': section,
                    'timestamp': latest['config'][section],
                })
                self.current['config'][section] = latest['config'][section]
