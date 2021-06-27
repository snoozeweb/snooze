#!/usr/bin/python36

from smtplib import SMTP
from email.header import Header
from email.mime.multipart import MIMEMultipart
from email.mime.text import MIMEText
from jinja2 import Template

from snooze.plugins.action import Action

from logging import getLogger
log = getLogger('snooze.action.mail')

DEFAULT_MESSAGE_TEMPLATE = """
*** Snooze ***
Received message:
{{ record.get('message') }}

{% if record.get('host') %}
Host: {{ record.get('host') }}
{% endif %}
{% if record.get('host') %}
Severity: {{ record.get('severity') }}
{% endif %}
"""

DEFAULT_SERVER = 'localhost'
DEFAULT_PORT = 25

class Mail(Action):
    def __init__(self, core):
        super().__init__(core)
        self.host = self.conf.get('host', DEFAULT_SERVER)
        self.port = self.conf.get('port', DEFAULT_PORT)
        self.sender = self.conf.get('sender', '')

    def pprint(self, content):
        output  =  'mailto: ' + content.get('to', '').replace(',', '\nmailto: ')
        return output

    def send(self, record, content):
        recipients = content.get('to', '').split(',')
        message = MIMEMultipart('alternative')
        msg = Template(content.get('message', DEFAULT_MESSAGE_TEMPLATE)).render(record)
        message['Subject'] = Header(Template(content.get('subject', '')).render(record), 'utf-8').encode()
        if self.sender:
            message['From'] = self.sender
        message['To'] = content.get('to', '')
        message['X-Priority'] = str(content.get('priority', 3))
        message.preamble = message['Subject']
        message.attach(MIMEText(msg, content.get('type', 'plain'), 'utf-8'))
        try:
            log.debug("Send mail to {}".format(message['To']))
            self.server = SMTP(self.host, self.port)
            self.server.sendmail(self.sender, recipients, message.as_string())
            self.server.close()
            self.core.stats.inc('snooze_notification_sent', {'type': self.name, 'name': content.get('name', '')})
        except Exception as e:
            self.core.stats.inc('snooze_notification_error', {'type': self.name, 'name': content.get('name', '')})
            log.exception(e)
