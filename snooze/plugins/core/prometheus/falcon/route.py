#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''Route for Prometheus webhook input plugin'''

from copy import deepcopy
from logging import getLogger

from bson.json_util import loads

from snooze.api.falcon import WebhookRoute
from snooze.utils.functions import sanitize

log = getLogger('snooze.webhooks.prometheus')

class PrometheusRoute(WebhookRoute):
    '''A webhook to receive Prometheus Alert Manager notifications'''
    auth = {
        'auth_disabled': True
    }

    def parse(self, content, media):
        '''Parse the data of the webhook to create an alert'''
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
                alert['tags'][tag_k] = sanitize(loads(tag_v))
            except Exception:
                alert['tags'][tag_k] = tag_v
        alert['annotations'] = {}
        for a_k, a_v in annotations.items():
            try:
                alert['annotations'][a_k] = sanitize(loads(a_v))
            except Exception:
                alert['annotations'][a_k] = a_v

        return alert

    def parse_webhook(self, req, media):
        alerts = []
        for alert_content in media.get('alerts', []):
            alert = self.parse(alert_content, media)
            alerts.append(alert)
        return alerts
