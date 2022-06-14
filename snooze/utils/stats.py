#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''A module for managing metrics in Prometheus'''

import contextlib
from datetime import datetime
from logging import getLogger
from typing import List

from prometheus_client import Summary, Counter, CollectorRegistry, generate_latest
from prometheus_client.context_managers import Timer

from snooze.utils.config import GeneralConfig
from snooze.db.database import AsyncIncrement

log = getLogger('snooze.stats')

class Stats():
    '''A wrapped backend for registrating and emitting metrics'''
    def __init__(self, core: 'Core', general: GeneralConfig):
        self.general = general
        self.enabled = general.metrics_enabled
        self.core = core
        self.database = core.db
        self.metrics = {}
        self.increment = AsyncIncrement(self.database, 'stats', 'value', upsert=True)
        self.core.threads['asyncdb'].new_increment(self.increment)
        if self.enabled:
            self.registry = CollectorRegistry()
            log.debug('Enabling Prometheus')

    def reload(self):
        '''Reload prometheus related configuration'''
        self.general.refresh()
        self.enabled = self.general.metrics_enabled
        log.debug('Prometheus server is %s', self.enabled)

    def init(self, metric: str, mtype: str, name: str, description: str, labels: List[str]):
        '''Register a type of metric'''
        if self.enabled:
            if mtype == 'summary':
                self.metrics[metric] = Summary(name, description, labels, registry=self.registry)
            elif mtype == 'counter':
                self.metrics[metric] = Counter(name, description, labels, registry=self.registry)
            else:
                log.error("Unsupported metric type %s, disabling", mtype)
                self.enabled = False

    def time(self, metric_name, labels):
        '''Emit a time measuring metric. Return a context manager that will measure the
        time spent inside of it'''
        metric = None
        if self.enabled and metric_name in self.metrics:
            metric = self.metrics[metric_name].labels(**labels)
            return Timer(metric, 'observe')
        return contextlib.suppress()

    def increment_db(self, metric_name: str, labels: dict, amount: int):
        now = datetime.utcnow().replace(minute=0, second=0, microsecond=0)
        for key, value in labels.items():
            metric_key = f"{metric_name}__{key}__{value}"
            search = {'date': now, 'key': metric_key}
            self.increment.increment(search, amount)

    def inc(self, metric_name, labels, amount=1):
        '''Increment a counter'''
        if self.enabled and metric_name in self.metrics:
            self.metrics[metric_name].labels(**labels).inc(amount)
            self.increment_db(metric_name, labels, amount)

    def get_metrics(self):
        return generate_latest(self.registry)

    def bootstrap(self):
        '''Bootstrap the general purpose metrics'''
        self.init(
            metric='process_alert_duration',
            mtype='summary',
            name='snooze_process_alert_duration',
            description='Average time spend processing a alert',
            labels=['source', 'environment', 'severity'],
        )
        self.init(
            metric='process_alert_duration_by_plugin',
            mtype='summary',
            name='snooze_process_alert_duration_by_plugin',
            description='Average time spend processing a alert by a given plugin',
            labels=['environment', 'plugin'],
        )
        self.init(
            metric='alert_hit',
            mtype='counter',
            name='snooze_alert_hit',
            description='Counter of received alerts',
            labels=['source', 'environment', 'severity'],
        )
        self.init(
            metric='alert_snoozed',
            mtype='counter',
            name='snooze_alert_snoozed',
            description='Counter of snoozed alerts',
            labels=['name'],
        )
        self.init(
            metric='alert_throttled',
            mtype='counter',
            name='snooze_alert_throttled',
            description='Counter of throttled alerts',
            labels=['name'],
        )
        self.init(
            metric='alert_closed',
            mtype='counter',
            name='snooze_alert_closed',
            description='Counter of received closed alerts',
            labels=['name'],
        )
        self.init(
            metric='notification_sent',
            mtype='counter',
            name='snooze_notification_sent',
            description='Counter of notification sent',
            labels=['name'],
        )
        self.init(
            metric='action_success',
            mtype='counter',
            name='snooze_action_success',
            description='Counter of action that succeeded',
            labels=['name'],
        )
        self.init(
            metric='action_error',
            mtype='counter',
            name='snooze_action_error',
            description='Counter of action that failed',
            labels=['name'],
        )
