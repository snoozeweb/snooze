#!/usr/bin/python
from logging import getLogger
log = getLogger('snooze.webhooks.influxdb')

from copy import deepcopy
from snooze.api.falcon import WebhookRoute
from snooze.utils.functions import sanitize
import json

class InfluxDBRoute(WebhookRoute):
    auth = {
        'auth_disabled': True
    }

    def parse(self, media):
        alert = {}

        media = sanitize(media)
        alert['raw'] = deepcopy(media)
        level = media.get('_level')
        if level == 'crit':
            level = 'critical'
        elif level == 'warn':
            level = 'warning'
        elif level == 'normal':
            level = 'ok'
        alert['check_name'] = media.get('_check_name', '')
        alert['notification_endpoint_name'] = media.get('_notification_endpoint_name', '')
        alert['notification_rule_name'] = media.get('_notification_rule_name', '')

        alert['process'] = media.get('process', media.get('_source_measurement'))
        alert['severity'] = media.get('severity', level)
        alert['message'] = media.get('_message', '')
        alert['source'] = 'influxdb2'
        for k, v in media.items():
            if k[0] != '_':
                try:
                    alert[k] = sanitize(json.loads(v))
                except:
                    alert[k] = v

        return alert

    def parse_webhook(self, req, media):
        alert = self.parse(media)
        return alert
