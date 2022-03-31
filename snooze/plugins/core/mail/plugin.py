#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

from smtplib import SMTP
from email.header import Header
from email.mime.multipart import MIMEMultipart
from email.mime.text import MIMEText
from jinja2 import Template

from snooze.plugins.core import Plugin

from logging import getLogger
log = getLogger('snooze.action.mail')

DEFAULT_MESSAGE_TEMPLATE = """
{% if record.get('host') %}
Host: {{ record.get('host') }}
{% endif %}
{% if record.get('source') %}
Source: {{ record.get('source') }}
{% endif %}
{% if record.get('process') %}
Process: {{ record.get('process') }}
{% endif %}
{% if record.get('severity') %}
Severity: {{ record.get('severity') }}
{% endif %}
Received message:
{{ record.get('message') }}
"""

DEFAULT_SERVER = 'localhost'
DEFAULT_PORT = 25
DEFAULT_TIMEOUT = 10

class Mail(Plugin):
    def pprint(self, content):
        output  =  'mailto: ' + content.get('to', '').replace(',', '\nmailto: ')
        return output

    def send(self, records, content):
        if not isinstance(records, list):
            records = [records]
        batch = content.get('batch', False) and len(records) > 1
        host = content.get('host', DEFAULT_SERVER)
        port = content.get('port', DEFAULT_PORT)
        sender = content.get('from', '')
        recipients = content.get('to', '').split(',')
        message = MIMEMultipart('alternative')
        if sender:
            message['From'] = sender
        message['To'] = content.get('to', '')
        message['X-Priority'] = str(content.get('priority', 3))
        log.debug("Send %s mail(s) to %s", len(records), message['To'])
        self.server = SMTP(host, port, timeout=DEFAULT_TIMEOUT)
        succeeded = []
        failed = []
        artifacts = []
        for record in records:
            try:
                artifact = {'records': [record]}
                artifact['body'] = Template(content.get('message', DEFAULT_MESSAGE_TEMPLATE)).render(record)
                if not batch:
                    artifact['subject'] = Header(Template(content.get('subject', '')).render(record), 'utf-8').encode()
                artifacts.append(artifact)
            except Exception as err:
                log.exception(err)
                failed.append(record)
        if batch:
            artifact = {'records': records}
            separator = {'plain': "\n\n", 'html': '<br><br>'}
            artifact['body'] = separator[content.get('type', 'plain')].join([a['body'] for a in artifacts])
            artifact['subject'] = f"[SnoozeWeb] Received {len(records)} alerts"
            artifacts = [artifact]
        for artifact in artifacts:
            message.set_payload([MIMEText(artifact['body'], content.get('type', 'plain'), 'utf-8')])
            message['Subject'] = artifact['subject']
            message.preamble = message['Subject']
            try:
                self.server.sendmail(sender, recipients, message.as_string())
                succeeded += artifact['records']
            except Exception as err:
                log.exception(err)
                failed += artifact['records']
        self.server.close()
        return succeeded, failed
