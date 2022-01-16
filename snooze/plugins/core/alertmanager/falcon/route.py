'''A module containing a route for interpreting Prometheus alertmanager notifications'''

from logging import getLogger
log = getLogger('snooze.webhooks.alertmanager')

from snooze.api.falcon import WebhookRoute
from snooze.utils.functions import sanitize
import json

class AlertmanagerRoute(WebhookRoute):
    auth = {
        'auth_disabled': True
    }

    def parse_webhook(self, req, media):
        '''Parse the request/media and return the alert'''
        alert = {}
        return alert
