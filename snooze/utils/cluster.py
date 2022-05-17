#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''A module for managing the snooze cluster, which is used for sharing configuration
file updates accross the cluster.'''

import socket
from logging import getLogger
from queue import Queue
from threading import Event
from typing import List, Tuple

import requests
import pkg_resources
from netifaces import interfaces, ifaddresses, AF_INET
from requests import Request, Response
from requests.adapters import HTTPAdapter, Retry
from requests.exceptions import RequestException

from snooze.utils.config import CoreConfig
from snooze.utils.exceptions import NonResolvableHost, SelfNotInCluster, SelfTooMuchInCluster
from snooze.utils.functions import ca_bundle
from snooze.utils.threading import SurvivingThread
from snooze.utils.typing import HostPort, PeerStatus

log = getLogger('snooze.cluster')

# Similar to urllib3.util.Retry in term of API
# https://urllib3.readthedocs.io/en/stable/reference/urllib3.util.html
# Will attempt 10 retries, with increasing interval between retries.
# intervals (in seconds) = [0.1, 0.2, 0.4, 1.6, 3.2, 6.4, 12.8, ..., 51.2]
ADAPTER = HTTPAdapter(max_retries=Retry(total=10, backoff_factor=0.1))

CACERT = ca_bundle()

class Cluster(SurvivingThread):
    '''A class representing the cluster and used for interacting with it.'''

    def __init__(self, core_config: CoreConfig, token: str, exit_event: Event = None):
        if exit_event is None:
            exit_event = Event()
        self.config = core_config.cluster
        self.token = token

        self.myself = HostPort(host=socket.gethostname(), port=core_config.port)
        self.others: List[HostPort] = []

        self.schema = 'https' if core_config.ssl.enabled else 'http'

        self.myself, self.others = who_am_i(self.config.members)

        self.queue = Queue()
        SurvivingThread.__init__(self, exit_event)

    def handle_query(self, req: Request) -> Response:
        '''Handle a request to other members of the cluster. We will not catch exceptions here
        because we want to fail if the retry doesn't work.'''
        session = requests.Session()
        session.mount(f"{self.schema}", ADAPTER)
        resp = session.send(req.prepare(), verify=CACERT, timeout=10)
        return resp

    def start_thread(self):
        while True:
            req: Request = self.queue.get()
            if req is ...: # Queue stopper
                self.queue.task_done()
                break
            self.handle_query(req)
            self.queue.task_done()
        log.info('Stopped cluster')

    def stop_thread(self):
        log.debug("Stopping cluster...")
        # Python's ellipsis (...) is used as a queue stopper, which is pretty
        # standard, and better than None which can happen accidentally, while ... is more rare.
        # and doesn't require a custom class.
        self.queue.put(...)
        self.queue.join()

    def status(self) -> PeerStatus:
        '''Return the status, health and info of the current node'''
        version = get_version()
        status = PeerStatus(
            host=self.myself.host,
            port=self.myself.port,
            version=version,
            healthy=True,
        )
        return status

    def members_status(self) -> List[PeerStatus]:
        '''Fetch the status of all members of the cluster'''
        statuses = []
        statuses.append(self.status())
        for member in self.others:
            try:
                url = f"{self.schema}://{member.host}:{member.port}/api/cluster?one"
                params = {}
                resp = requests.get(url, params=params, verify=CACERT, timeout=10)
                resp.raise_for_status()
                data = resp.json()['data'][0]
                status = PeerStatus(**data)
            except RequestException as err:
                status = PeerStatus(
                    host=member.host,
                    port=member.port,
                    version='unknown',
                    healthy=False,
                    error=str(err),
            )
            statuses.append(status)
        return statuses

    def sync_reload_plugin(self, plugin_name: str):
        '''Async function to ask other members to reload the configuration of a plugin'''
        for member in self.others:
            site = f"{self.schema}://{member.host}:{member.port}"
            self.queue.put(RequestReloadPlugin(site, plugin_name, self.token))

    def sync_setting_update(self, section: str, data: dict, auth: str):
        '''Async function to ask other members to update their configuration and reload'''
        for member in self.others:
            site = f"{self.schema}://{member.host}:{member.port}"
            self.queue.put(RequestSettingUpdate(site, section, data, auth))

class RequestReloadPlugin(Request):
    '''Request another member to reload a given plugin'''
    def __init__(self, site: str, plugin_name: str, token: str):
        url = f"{site}/api/reload/{plugin_name}"
        headers = {'Authorization': f"JWT {token}"}
        Request.__init__(self, 'POST', url, headers=headers)

class RequestSettingUpdate(Request):
    '''Request another member to rewrite a config'''
    def __init__(self, site: str, section: str, data: dict, auth: str):
        url = f"{site}/api/settings/{section}"
        # Forwarding the authentication to the next peer
        headers = {'Authorization': auth}
        Request.__init__(self, 'PUT', url, headers=headers, json=data)

def who_am_i(members: List[HostPort]) -> Tuple[HostPort, List[HostPort]]:
    '''Return which member of the cluster the running program is.
    Raise exceptions in the following cases:
    * NonResolvableHost: if one member of the cluster has its DNS not resolvable
    * SelfNotInCluster: if the running node cannot be found in the cluster
    * SelfTooMuchInCluster: if the running node is found more than one time in the cluster
    '''
    my_addresses = [
        link.get('addr')
        for interface in interfaces()
        for link in ifaddresses(interface).get(AF_INET, [])
    ]
    matches = []
    for member in members:
        try:
            host = socket.gethostbyname(member.host)
            if host in my_addresses:
                matches.append(member)
        except socket.gaierror as err:
            raise NonResolvableHost(member.host) from err
    if len(matches) == 1:
        myself = matches[0]
        return myself, [x for x in members if x != myself]
    elif len(matches) == 0:
        raise SelfNotInCluster()
    else:
        raise SelfTooMuchInCluster()

def get_version() -> str:
    '''Return the version of the installed snooze-server. Return 'unknown' if not found'''
    try:
        return pkg_resources.get_distribution('snooze-server').version
    except pkg_resources.DistributionNotFound as err:
        log.exception(err)
        return 'unknown'
