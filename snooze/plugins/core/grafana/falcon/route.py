#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''Webhook input plugin for Grafana notifications'''

from logging import getLogger

from bson.json_util import loads

from snooze.api.routes import WebhookRoute
from snooze.utils.functions import sanitize

log = getLogger('snooze-process')

class GrafanaRoute(WebhookRoute):
    '''A falcon route to receive Grafana alerts as input'''
    authentication = False

    def parse_old(self, match, media):
        '''Grafana 8.4 and lower. Parse the data of the webhook to create an alert'''
        alert = {}
        tags = match.get('tags') or {}
        alert['metric'] = match.get('metric', '')
        alert['value'] = match.get('value', '')
        alert['image_url'] = media.get('imageUrl', '')
        alert['rule_id'] = media.get('ruleId', '')
        alert['rule_url'] = media.get('ruleUrl', '')
        alert['panel_id'] = media.get('panelId', '')
        alert['dashboard_id'] = media.get('dashboardId', '')
        alert['org_id'] = media.get('orgId', '')
        alert['rule_name'] = media.get('ruleName', '')

        alert['host'] = tags.pop('host', media.get('ruleName', ''))
        alert['process'] = tags.pop('process', match.get('metric', ''))
        alert['severity'] = tags.pop('severity', 'critical')
        alert['message'] = media.get('message', media.get('title', media.get('rule_name', '')))
        alert['source'] = 'grafana'
        alert['tags'] = {}
        alert['raw'] = sanitize(match)
        for tag_k, tag_v in tags.items():
            try:
                alert['tags'][tag_k] = loads(tag_v)
            except Exception:
                alert['tags'][tag_k] = tag_v
        alert['tags'].update(media.get('tags') or {})
        alert['tags'] = sanitize(alert['tags'])

        return alert

    def parse_new(self, match, media):
        '''Grafana 8.5 and higher. Parse the data of the webhook to create an alert'''
        alert = {}
        labels = match.get('labels') or {}
        values = match.get('values') or {}
        annotations = match.get('annotations') or {}
        alert['generator_url'] = match.get('generatorURL', '')
        alert['fingerprint'] = match.get('fingerprint', '')
        alert['dashboard_url'] = match.get('dashboardURL', '')
        alert['panel_url'] = match.get('panelURL', '')
        alert['value_string'] = match.get('valueString', '')
        alert['description'] = annotations.pop('description', '')

        alert['host'] = labels.pop('host', labels.pop('instance', ''))
        alert['process'] = labels.pop('process', labels.pop('alertname', ''))
        if match.get('status') == 'resolved':
            alert['severity'] = 'ok'
        else:
            alert['severity'] = labels.pop('severity', 'critical')
        alert['message'] = annotations.pop('message', annotations.pop('summary', ''))
        alert['source'] = 'grafana'
        alert['raw'] = sanitize(match)
        alert['labels'] = {}
        for label_k, label_v in labels.items():
            try:
                alert['labels'][label_k] = loads(label_v)
            except Exception:
                alert['labels'][label_k] = label_v
        alert['labels'].update(media.get('labels') or {})
        alert['labels'] = sanitize(alert['labels'])
        alert['values'] = {}
        for value_k, value_v in values.items():
            try:
                alert['values'][value_k] = loads(value_v)
            except Exception:
                alert['values'][value_k] = value_v
        alert['values'].update(media.get('values') or {})
        alert['values'] = sanitize(alert['values'])
        alert['annotations'] = {}
        for annotation_k, annotation_v in annotations.items():
            try:
                alert['annotations'][annotation_k] = loads(annotation_v)
            except Exception:
                alert['annotations'][annotation_k] = annotation_v
        alert['annotations'].update(media.get('annotations') or {})
        alert['annotations'] = sanitize(alert['annotations'])

        return alert

    def parse_webhook(self, req, media):
        alerts = []
        if media.get('state', '') == 'alerting':
            for match in media.get('evalMatches', []):
                alert = self.parse_old(match, media)
                alerts.append(alert)
            for match in media.get('alerts', []):
                alert = self.parse_new(match, media)
                alerts.append(alert)
        return alerts
