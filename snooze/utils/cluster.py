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
from threading import Event
from typing import List, Optional

from dataclasses import dataclass

import netifaces
import pkg_resources
import bson.json_util

from snooze.utils.threading import SurvivingThread
from snooze.utils.config import CoreConfig

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
    def __init__(self, core_config: CoreConfig, reload_token: str, exit_event: Optional[Event] = None):
        self.config = core_config.cluster
        self.core_config = core_config
        self.reload_token = reload_token
        self.sync_queue = []
        self.other_peers = []
        self.enabled = self.config.enabled
        if self.enabled:
            log.debug('Init Cluster Manager')
            self.all_peers = self.config.members
            self.other_peers = self.config.members
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
        SurvivingThread.__init__(self, exit_event)

    def status(self) -> PeerStatus:
        '''Return the status, health and info of the current node'''
        if self.enabled:
            host = self.self_peer[0]['host']
            port = self.self_peer[0]['port']
        else:
            host = socket.gethostname()
            port = self.core_config.port
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
            use_ssl = self.core_config.ssl.enabled
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
        use_ssl = self.core_config.ssl.enabled
        success = False
        while not self.exit.wait(0.1):
            for index, job in enumerate(self.sync_queue):
                if use_ssl:
                    connection = http.client.HTTPSConnection(job['host'], job['port'], timeout=10)
                else:
                    connection = http.client.HTTPConnection(job['host'], job['port'], timeout=10)
                job['payload'].update({'reload_token': self.reload_token})
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
