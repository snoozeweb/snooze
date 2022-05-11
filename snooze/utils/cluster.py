#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''A module for managing the snooze cluster, which is used for sharing configuration
file updates accross the cluster.'''

import http.client
import os
import socket
import time
from logging import getLogger
from typing import List

from dataclasses import dataclass

import netifaces
import pkg_resources
import bson.json_util

from snooze.utils.threading import SurvivingThread

log = getLogger('snooze.cluster')

@dataclass
class PeerStatus:
    '''A dataclass containing the status of one peer'''
    host: str
    port: str
    version: str
    healthy: bool

def get_version() -> str:
    '''Return the version of the installed snooze-server. Return 'unknown' if not found'''
    try:
        return pkg_resources.get_distribution('snooze-server').version
    except pkg_resources.DistributionNotFound as err:
        log.exception(err)
        return 'unknown'

class Cluster(SurvivingThread):
    '''A class representing the cluster and used for interacting with it.'''
    def __init__(self, core: 'Core'):
        self.core = core
        env_cluster = os.environ.get('SNOOZE_CLUSTER')
        self.conf = {}
        if env_cluster:
            try:
                env_cluster = env_cluster.split(',')
                self.conf['enabled'] = True
                self.conf['members'] = []
                for member in env_cluster:
                    host, *port = member.split(':')
                    self.conf['members'].append({'host': host, 'port': port[0] if port else 5200})
            except Exception as err:
                log.exception(err)
                log.warning('Error when parsing cluster config defined in SNOOZE_CLUSTER env var')
                self.conf = {}
        if not self.conf:
            self.conf = core.conf.get('clustering', {})
        self.sync_queue = []
        self.other_peers = []
        self.enabled = self.conf.get('enabled', False)
        if self.enabled:
            log.debug('Init Cluster Manager')
            self.all_peers = self.conf.get('members', [])
            self.other_peers = self.conf.get('members', [])
            for interface in netifaces.interfaces():
                for arr in netifaces.ifaddresses(interface).values():
                    for line in arr:
                        try:
                            self.other_peers = [
                                x for x in self.other_peers
                                if socket.gethostbyname(x['host']) != line['addr']
                            ]
                        except Exception as err:
                            log.exception(err)
                            log.error('Error while setting up the cluster. Disabling cluster...')
                            self.enabled = False
                            return
            self.self_peer = [peer for peer in self.all_peers if peer not in self.other_peers]
            if len(self.self_peer) != 1:
                log.error("This node was found %d time(s) in the cluster configuration. Disabling cluster...",
                    len(self.self_peer))
                self.enabled = False
                return
            log.debug("Other peers: %s", self.other_peers)
        SurvivingThread.__init__(self, core.exit_event)

    def status(self) -> PeerStatus:
        '''Return the status, health and info of the current node'''
        if self.enabled:
            host = self.self_peer[0]['host']
            port = self.self_peer[0]['port']
        else:
            host = socket.gethostname()
            port = self.core.conf.get('port', '5200')
        version = get_version()
        self_peer = PeerStatus(host, port, version, True)
        log.debug("Self cluster configuration: %s", self_peer)
        return self_peer

    def members_status(self) -> List[PeerStatus]:
        '''Fetch the status of all members of the cluster'''
        members = []
        members.append(self.status())
        if self.enabled:
            success = False
            use_ssl = self.core.conf.get('ssl', {}).get('enabled', False)
            for peer in self.other_peers:
                if use_ssl:
                    connection = http.client.HTTPSConnection(peer['host'], peer['port'], timeout=10)
                else:
                    connection = http.client.HTTPConnection(peer['host'], peer['port'], timeout=10)
                try:
                    connection.request('GET', '/api/cluster?self=true')
                    response = connection.getresponse()
                    success = (response.status == 200)
                except Exception as err:
                    log.exception(err)
                    success = False
                host = peer['host']
                port = peer['port']
                version = 'unknown'
                healthy = success
                try:
                    json_data = bson.json_util.loads(response.read().decode()).get('data')[0]
                    host = json_data.get('host', peer['host'])
                    port = json_data.get('port', peer['port'])
                    version = json_data.get('version', 'unknown')
                except Exception as err:
                    log.exception(err)
                peer = PeerStatus(host, port, version, healthy)
                members.append(peer)
            log.debug("Cluster members: %s", members)
            return members
        else:
            return members

    def reload_plugin(self, plugin_name):
        '''Ask other members to reload the configuration of a plugin'''
        for peer in self.other_peers:
            job = {'payload': {'reload': {'plugins':[plugin_name]}}, 'host': peer['host'], 'port': peer['port']}
            self.sync_queue.append(job)
            log.debug("Queued job: %s", job)

    def write_and_reload(self, filename, conf, reload_conf):
        '''Ask other members to update their configuration and reload'''
        for peer in self.other_peers:
            job = {
                'payload': {'filename': filename, 'conf': conf, 'reload': reload_conf},
                'host': peer['host'],
                'port': peer['port'],
            }
            self.sync_queue.append(job)
            log.debug("Queued job: %s", job)

    def start_thread(self):
        headers = {'Content-type': 'application/json'}
        use_ssl = self.core.conf.get('ssl', {}).get('enabled', False)
        success = False
        while not self.exit.wait(0.1):
            for index, job in enumerate(self.sync_queue):
                if use_ssl:
                    connection = http.client.HTTPSConnection(job['host'], job['port'], timeout=10)
                else:
                    connection = http.client.HTTPConnection(job['host'], job['port'], timeout=10)
                job['payload'].update({'reload_token': self.core.secrets.get('reload_token', '')})
                job_json = bson.json_util.dumps(job['payload'])
                try:
                    connection.request('POST', '/api/reload', job_json, headers)
                    response = connection.getresponse()
                    success = (response.status == 200)
                except Exception as err:
                    log.exception(err)
                    success = False
                job['payload'].pop('reload_token')
                if success:
                    del self.sync_queue[index]
                    log.debug("Dequeued job: %s", job)
                else:
                    log.error("Could not dequeue job: %s", job)
            time.sleep(1)
        log.info('Stopped cluster')
