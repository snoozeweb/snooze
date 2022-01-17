#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

#!/usr/bin/python
from logging import getLogger
log = getLogger('snooze.webhooks.kapacitor')

from snooze.api.falcon import WebhookRoute
from snooze.utils.functions import sanitize
import json

class KapacitorRoute(WebhookRoute):
    auth = {
        'auth_disabled': True
    }

    def parse(self, match, media):
        alert = {}
        tags = match.get('tags') or {}
        alert['columns'] = match.get('columns', [])
        alert['values'] = match.get('values', [])
        alert['details'] = match.get('details', '')

        alert['host'] = tags.pop('host', '')
        alert['process'] = tags.pop('process', media.get('id', ''))
        alert['severity'] = tags.pop('severity', media.get('level', 'critical'))
        alert['message'] = media.get('message', '')
        alert['source'] = 'kapacitor'
        alert['raw'] = media
        for tag_k, tag_v in tags.items():
            try:
                alert['tags'][tag_k] = sanitize(json.loads(tag_v))
            except:
                alert['tags'][tag_k] = tag_v
        alert['tags'].update(media.get('tags') or {})

        return alert

    def parse_webhook(self, req, media):
        alerts = []
        media = sanitize(media)
        for match in media.get('data', {}).get('series', []):
            alert = self.parse(match, media)
            alerts.append(alert)
        return alerts
