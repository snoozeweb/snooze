#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''A module with routes for getting the health of the snooze server'''

from typing import List
from logging import getLogger

from typing_extensions import Literal

import falcon

from snooze.api.base import BasicRoute

log = getLogger('snooze.health')

Health = Literal['ok', 'warning', 'critical', 'unknown']

def thread_status(core: 'Core', status: dict) -> List[Health]:
    '''Get the status of threads in the core'''
    status['threads'] = {}
    healths = []
    for name, thread in core.threads.items():
        alive = thread.is_alive()
        if not alive:
            healths.append('critical')
            status['issues'].append(f"Thread '{name}' is not alive")
        else:
            healths.append('ok')
        status['threads'][name] = {
            'alive': alive,
        }
    return healths

def mq_status(mq_manager: 'MQManager', status: dict) -> List[Health]:
    '''Compute the status of the MQManager'''
    status['mq'] = {}
    status['mq']['threads'] = {}
    healths = []
    for name, thread in mq_manager.threads.items():
        alive = thread.is_alive()
        if not alive:
            healths.append('warning')
            status['issues'].append('')
        else:
            healths.append('ok')
        status['mq']['threads'][name] = {'alive': alive}
    return healths

class HealthRoute(BasicRoute):
    '''A falcon route that return the health of the snooze server'''
    auth = {'auth_disabled': True}

    def on_get(self, req, resp):
        status = {}
        status['issues'] = []
        healths = []

        try:
            healths += thread_status(self.core, status)
            healths += mq_status(self.core.mq, status)
        except Exception as err:
            status['health'] = 'unknown'
            status['issues'] = ["f{err.__class__.__name__}: {err}"]
            log.warning(err)
            resp.media = status
            resp.status = falcon.HTTP_503
            raise err
            return

        resp.media = status

        if any(h == 'critical' for h in healths):
            status['health'] = 'critical'
            resp.status = falcon.HTTP_503
        elif any(h == 'warning' for h in healths):
            status['health'] = 'warning'
            resp.status = falcon.HTTP_503
        elif all(h == 'ok' for h in healths):
            status['health'] = 'ok'
            resp.status = falcon.HTTP_OK
        else:
            status['health'] = 'unknown'
            resp.status = falcon.HTTP_503
