#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

#!/usr/bin/python3.6

import logging
import time
import threading
import http.client
import netifaces
import socket
import os
import pkg_resources

import bson.json_util
from logging import getLogger
from snooze.utils import config
log = getLogger('snooze.cluster')

class Cluster():
    def __init__(self, api):
        self.api = api
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
            except Exception as e:
                log.exception(e)
                log.warning('Error when parsing cluster config defined in SNOOZE_CLUSTER env var')
                self.conf = {}
        if not self.conf:
            self.conf = api.core.conf.get('clustering', {})
        self.thread = None
        self.sync_queue = []
        self.enabled = self.conf.get('enabled', False)
        if self.enabled:
            log.debug('Init Cluster Manager')
            self.all_peers = self.conf.get('members', [])
            self.other_peers = self.conf.get('members', [])
            for interface in netifaces.interfaces():
                for arr in netifaces.ifaddresses(interface).values():
                    for line in arr:
                        try:
                            self.other_peers = list(filter(lambda x: socket.gethostbyname(x['host']) != line['addr'], self.other_peers))
                        except Exception as e:
                            log.exception(e)
                            log.error('Error while setting up the cluster. Disabling cluster...')
                            self.enabled = False
                            return
            self.self_peer = [peer for peer in self.all_peers if peer not in self.other_peers]
            if len(self.self_peer) != 1:
                log.error("This node was found {} time(s) in the cluster configuration. Disabling cluster...".format(len(self.self_peer)))
                self.enabled = False
                return
            log.debug("Other peers: {}".format(self.other_peers))
            self.thread = ClusterThread(self)
            self.thread.start()

    def get_self(self, caller = False):
        if self.enabled:
            self_peer = [{'host': self.self_peer[0]['host'], 'port': self.self_peer[0]['port'], 'version': self.get_version(), 'healthy': True, 'caller': caller}]
            log.debug("Self cluster configuration: {}".format(self_peer))
            return self_peer
        else:
            try:
                hostname = socket.gethostname()
            except Exception as e:
                log.exception(e)
                hostname = 'unknown'
            return [{'host': hostname, 'port': self.api.core.conf.get('port', '5200'), 'version': self.get_version(), 'healthy': True, 'caller': True}]

    def get_members(self):
        if self.enabled:
            success = False
            members = self.get_self(True)
            use_ssl = self.api.core.conf.get('ssl', {}).get('enabled', False)
            for peer in self.other_peers:
                if use_ssl:
                    connection = http.client.HTTPSConnection(peer['host'], peer['port'], timeout=10)
                else:
                    connection = http.client.HTTPConnection(peer['host'], peer['port'], timeout=10)
                try:
                    connection.request('GET', '/api/cluster?self=true')
                    response = connection.getresponse()
                    success = (response.status == 200)
                except Exception as e:
                    log.exception(e)
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
                except Exception as e:
                    log.exception(e)
                members.append({'host': host, 'port': port, 'version': version, 'healthy': healthy})
            log.debug("Cluster members: {}".format(members))
            return members
        else:
            return self.get_self()

    def reload_plugin(self, plugin_name):
        if self.thread:
            for peer in self.other_peers:
                job = {'payload': {'reload': {'plugins':[plugin_name]}}, 'host': peer['host'], 'port': peer['port']}
                self.sync_queue.append(job)
                log.debug("Queued job: {}".format(job))

    def write_and_reload(self, filename, conf, reload_conf):
        if self.thread:
            for peer in self.other_peers:
                job = {'payload': {'filename': filename, 'conf': conf, 'reload': reload_conf}, 'host': peer['host'], 'port': peer['port']}
                self.sync_queue.append(job)
                log.debug("Queued job: {}".format(job))

    def get_version(self):
        try:
            return pkg_resources.get_distribution('snooze-server').version
        except Exception as e:
            log.exception(e)
            return 'unknown'

class ClusterThread(threading.Thread):

    def __init__(self, cluster):
        super().__init__()
        self.cluster = cluster
        self.main_thread = threading.main_thread()

    def run(self):
        headers = {'Content-type': 'application/json'}
        use_ssl = self.cluster.api.core.conf.get('ssl', {}).get('enabled', False)
        success = False
        while True:
            if not self.main_thread.is_alive():
                break
            for index, job in enumerate(self.cluster.sync_queue):
                if use_ssl:
                    connection = http.client.HTTPSConnection(job['host'], job['port'], timeout=10)
                else:
                    connection = http.client.HTTPConnection(job['host'], job['port'], timeout=10)
                job['payload'].update({'reload_token': self.cluster.api.core.secrets.get('reload_token', '')})
                job_json = bson.json_util.dumps(job['payload'])
                try:
                    connection.request('POST', '/api/reload', job_json, headers)
                    response = connection.getresponse()
                    success = (response.status == 200)
                except Exception as e:
                    log.exception(e)
                    success = False
                job['payload'].pop('reload_token')
                if success:
                    del self.cluster.sync_queue[index]
                    log.debug("Dequeued job: {}".format(job))
                else:
                    log.error("Could not dequeue job: {}".format(job))
            time.sleep(1)
