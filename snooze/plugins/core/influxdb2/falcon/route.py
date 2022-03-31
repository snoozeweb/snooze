#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''A webhook for influxdb v2 notifications'''

from copy import deepcopy
from logging import getLogger

from bson.json_util import loads

from snooze.api.falcon import WebhookRoute
from snooze.utils.functions import sanitize

log = getLogger('snooze.webhooks.influxdb')

class InfluxDBRoute(WebhookRoute):
    '''A falcon route to handle InfluxDB v2 alerts'''
    auth = {
        'auth_disabled': True
    }

    def parse(self, media):
        '''Parse the data of the webhook to create an alert'''
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
        for key, value in media.items():
            if key[0] != '_':
                try:
                    alert[key] = sanitize(loads(value))
                except Exception:
                    alert[key] = value

        return alert

    def parse_webhook(self, req, media):
        alert = self.parse(media)
        return alert
