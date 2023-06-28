#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''Route for AlertManager webhook input plugin'''

from logging import getLogger
from datetime import datetime
from typing import Optional, Literal, Dict, List, Any

from pydantic import BaseModel, Field

from snooze.api.routes import WebhookRoute
from snooze.utils.functions import sanitize

log = getLogger('snooze.webhooks.prometheus')

class AM4Alert(BaseModel):
    '''Represent an alert within an AlertManager V4 hook'''
    status: Literal['firing', 'resolved']
    labels: Dict[str, str] = Field(default_factory=dict)
    annotations: Dict[str, str] = Field(default_factory=dict)
    startsAt: Optional[datetime] = None
    endsAt: Optional[datetime] = None
    generatorURL: Optional[str] = None
    fingerprint: Optional[str] = None

class AM4Webhook(BaseModel):
    '''Represent an AlertManager V4 data'''
    version: Literal['4']
    receiver: Optional[str] = None
    status: Literal['firing', 'resolved']
    commonLabels: Dict[str, str] = Field(default_factory=dict)
    groupLabels: Dict[str, str] = Field(default_factory=dict)
    commonAnnotations: Dict[str, str] = Field(default_factory=dict)
    generatorURL: Optional[str] = None
    externalURL: Optional[str] = None
    groupKey: Optional[str] = None
    trucatedAlerts: Optional[int] = 0
    alerts: List[AM4Alert]

class AlertManagerV4Route(WebhookRoute):
    '''A webhook to receive Prometheus Alert Manager notifications'''
    authentication = False

    def parse_webhook(self, req, media) -> List[Dict[str, Any]]:
        '''Parse the data of the webhook to create an alert'''
        new_alerts: List[Dict[str, Any]] = []
        webhook = AM4Webhook.parse_obj(media)
        for alert in webhook.alerts:
            new_alert: Dict[str, Any] = {}
            new_alert['source'] = 'AlertManager'

            annotations: Dict[str, str] = {}
            annotations.update(webhook.commonAnnotations)
            annotations.update(alert.annotations)
            new_alert['annotations'] = sanitize(annotations)

            labels: Dict[str, str] = {}
            labels.update(webhook.commonLabels)
            labels.update(webhook.groupLabels)
            labels.update(alert.labels)
            new_alert['labels'] = sanitize(labels)

            # Severity
            if webhook.status == 'firing':
                new_alert['severity'] = alert.labels.get('severity') \
                    or webhook.commonLabels.get('severity') \
                    or 'critical'
            elif webhook.status == 'resolved':
                new_alert['severity'] = 'ok'

            new_alert['generatorURL'] = alert.generatorURL or webhook.generatorURL
            new_alert['externalURL'] = webhook.externalURL

            new_alert['host'] = labels.get('host') \
                or labels.get('instance') \
                or labels.get('exported_instance') \
                or '-'
            new_alert['process'] = labels.get('process') \
                or labels.get('service') \
                or labels.get('alertgroup') \
                or labels.get('job') \
                or '-'
            new_alert['message'] = annotations.get('message') \
                or annotations.get('summary') \
                or annotations.get('description') \
                or annotations.get('externalURL') \
                or ''

            new_alerts.append(new_alert)
        return new_alerts
