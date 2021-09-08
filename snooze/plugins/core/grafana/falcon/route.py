#!/usr/bin/python
from logging import getLogger
log = getLogger('snooze.webhooks.grafana')

from snooze.api.falcon import WebhookRoute
from snooze.utils.functions import sanitize

class GrafanaRoute(WebhookRoute):
    auth = {
        'auth_disabled': True
    }

    def parse_grafana(self, match, params, media):
        alert = {}
        for key,val in params:
            alert[key.replace('.', '_')] = val
        alert['tags'] = match.get('tags') or {}
        alert['tags'].update(media.get('tags') or {})
        alert['metric'] = match.get('metric', '')
        alert['value'] = match.get('value', '')
        alert['image_url'] = media.get('imageUrl', '')
        alert['rule_id'] = media.get('ruleId', '')
        alert['rule_url'] = media.get('ruleUrl', '')
        alert['panel_id'] = media.get('panelId', '')
        alert['dashboard_id'] = media.get('dashboardId', '')
        alert['org_id'] = media.get('orgId', '')
        alert['rule_name'] = media.get('ruleName', '')

        alert['host'] = alert['tags'].get('host', media.get('ruleName', ''))
        alert['process'] = alert['tags'].get('process', match.get('metric', ''))
        alert['severity'] = alert['tags'].get('severity', 'critical')
        alert['message'] = media.get('message', media.get('title', media.get('rule_name', '')))
        alert['source'] = 'grafana'
        alert['raw'] = sanitize(media)
        return alert

    def parse_webhook(self, req, media):
        alerts = []
        if media.get('state', '') == 'alerting':
            for match in media.get('evalMatches', []):
                alert = self.parse_grafana(match, req.params, media)
                alerts.append(alert)
        return alerts
