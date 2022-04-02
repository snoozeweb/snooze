#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

#!/usr/bin/python3.6

from prometheus_client import start_http_server, Summary, Counter, CollectorRegistry, generate_latest
from prometheus_client.context_managers import Timer

import logging
import datetime
import contextlib

from logging import getLogger
from snooze.utils import config
log = getLogger('snooze.stats')

class Stats():
    def __init__(self, core):
        self.core = core
        self.conf = self.core.general_conf
        self.metrics = {}
        self.reload()
        if self.enabled:
            self.registry = CollectorRegistry()
            log.debug('Enabling Prometheus')

    def reload(self):
        self.enabled = self.conf.get('metrics_enabled', True)
        log.debug('Prometheus server is {}'.format(self.enabled))

    def init(self, metric, mtype, name, desc, labels):
        if self.enabled:
            if mtype == 'summary':
                self.metrics[metric] = Summary(name, desc, labels, registry=self.registry)
            elif mtype == 'counter':
                self.metrics[metric] = Counter(name, desc, labels, registry=self.registry)
            else:
                log.error("Unsupported metric type {}, disabling".format(mtype))
                self.enabled = False

    def time(self, metric_name, labels):
        metric = None
        if self.enabled and metric_name in self.metrics:
            metric = self.metrics[metric_name].labels(**labels)
            return Timer(metric, 'observe')
        return contextlib.suppress()

    def inc(self, metric_name, labels, amount = 1):
        if self.enabled and metric_name in self.metrics:
            self.metrics[metric_name].labels(**labels).inc(amount)
            self.core.db.inc('stats', metric_name, labels)

    def get_metrics(self):
        return generate_latest(self.registry)
