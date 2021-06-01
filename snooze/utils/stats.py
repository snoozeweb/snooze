#!/usr/bin/python3.6

from prometheus_client import start_http_server, Summary, Counter
from prometheus_client.context_managers import Timer

import logging
import datetime

from logging import getLogger
from snooze.utils import config
log = getLogger('snooze.stats')

class Stats():
    def __init__(self, auto_enable=None):
        self.metrics = {}
        if auto_enable is not None:
            self.enabled = auto_enable
        else:
            self.reload()
        if self.enabled:
            port = self.conf.get('port', 9234)
            log.debug('Starting Prometheus server on port {}'.format(port))
            start_http_server(port)

    def reload(self):
        self.conf = config('stats')
        self.enabled = self.conf.get('enabled', True)
        log.debug('Prometheus server is {}'.format(self.enabled))

    def init(self, metric):
        if self.enabled:
            if metric == 'process_record_duration':
                self.metrics[metric] = Summary(
                    'snooze_record_process_duration',
                    'Average time spend processing a record',
                    ['source'],
                )
            elif metric == 'notification_sent':
                self.metrics[metric] = Counter(
                    'snooze_notification_sent',
                    'Counter of notification sent',
                    ['name'],
                )
            elif metric == 'notification_error':
                self.metrics[metric] = Counter(
                    'snooze_notification_error',
                    'Counter of notification that failed',
                    ['name'],
                )

    def time(self, metric_name, labels):
        metric = None
        if self.enabled and metric_name in self.metrics:
            metric = self.metrics[metric_name].labels(**labels)
        return Timer(metric.observe if metric else (lambda x: x))

    def inc(self, metric_name, labels):
        if self.enabled and metric_name in self.metrics:
            self.metrics[metric_name].labels(**labels).inc()
