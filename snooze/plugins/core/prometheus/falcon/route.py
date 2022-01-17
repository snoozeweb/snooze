#!/usr/bin/python
from logging import getLogger
log = getLogger('snooze.webhooks.prometheus')

from copy import deepcopy
from snooze.api.falcon import WebhookRoute
from snooze.utils.functions import sanitize
import json

class PrometheusRoute(WebhookRoute):
    auth = {
        'auth_disabled': True
    }

    def parse(self, content, media):
        alert = {}

        content = sanitize(content)
        alert['raw'] = deepcopy(content)
        status = content.get('status', 'firing')
        labels = content.get('labels', {})
        annotations = content.get('annotations', {})
        alert['prometheus'] = {}
        if 'externalURL' in media:
            alert['prometheus']['externalURL'] = media.get('externalURL')
        if 'generatorURL' in content:
            alert['prometheus']['generatorURL'] = content.get('generatorURL')
        alert['prometheus']['startsAt'] = content.get('startsAt', '')
        alert['prometheus']['endsAt'] = content.get('endsAt', '')

        if status == 'firing':
            alert['severity'] = labels.pop('severity', 'critical')
        elif status == 'resolved':
            alert['severity'] = 'ok'
        else:
            alert['severity'] = 'unknown'

        alert['host'] = labels.pop('host', labels.pop('instance', labels.pop('exported_instance', '')))
        alert['process'] = labels.pop('process', labels.pop('service'))
        alert['source'] = 'prometheus'
        alert['message'] = annotations.pop('message', annotations.pop('summary', annotations.pop('description', annotations.get('externalURL', ''))))
        alert['tags'] = {}
        for tag_k, tag_v in labels.items():
            try:
                alert['tags'][tag_k] = sanitize(json.loads(tag_v))
            except:
                alert['tags'][tag_k] = tag_v
        alert['annotations'] = {}
        for a_k, a_v in annotations.items():
            try:
                alert['annotations'][a_k] = sanitize(json.loads(a_v))
            except:
                alert['annotations'][a_k] = a_v

        return alert

    def parse_webhook(self, req, media):
        alerts = []
        for alert_content in media.get('alerts', []):
            alert = self.parse(alert_content, media)
            alerts.append(alert)
        return alerts
