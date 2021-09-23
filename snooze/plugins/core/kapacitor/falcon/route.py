#!/usr/bin/python
from logging import getLogger
log = getLogger('snooze.webhooks.kapacitor')

from snooze.api.falcon import WebhookRoute
from snooze.utils.functions import sanitize

class KapacitorRoute(WebhookRoute):
    auth = {
        'auth_disabled': True
    }

    def parse_kapacitor(self, match, params, media):
        alert = {}
        for key,val in params:
            alert[key.replace('.', '_')] = val
        alert['tags'] = match.get('tags') or {}
        alert['tags'].update(media.get('tags') or {})
        alert['columns'] = match.get('columns', [])
        alert['values'] = match.get('values', [])
        alert['details'] = match.get('details', '')

        alert['host'] = alert['tags'].get('host', '')
        alert['process'] = alert['tags'].get('process', media.get('id', ''))
        alert['severity'] = alert['tags'].get('severity', 'critical')
        alert['message'] = media.get('message', '')
        alert['source'] = 'kapacitor'
        alert['raw'] = sanitize(media)
        return alert

    def parse_webhook(self, req, media):
        alerts = []
        if media.get('level', '') == 'CRITICAL':
            for match in media.get('data', {}).get('series', []):
                alert = self.parse_kapacitor(match, req.params, media)
                alerts.append(alert)
        return alerts
