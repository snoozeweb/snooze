#!/usr/bin/python36

from smtplib import SMTP
from email.header import Header
from email.mime.multipart import MIMEMultipart
from email.mime.text import MIMEText
from jinja2 import Template

from snooze.plugins.core import Plugin

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
DEFAULT_TIMEOUT = 10

class Mail(Plugin):
    def pprint(self, content):
        output  =  'mailto: ' + content.get('to', '').replace(',', '\nmailto: ')
        return output

    def send(self, record, content):
        host = content.get('host', DEFAULT_SERVER)
        port = content.get('port', DEFAULT_PORT)
        sender = content.get('from', '')
        recipients = content.get('to', '').split(',')
        message = MIMEMultipart('alternative')
        msg = Template(content.get('message', DEFAULT_MESSAGE_TEMPLATE)).render(record)
        message['Subject'] = Header(Template(content.get('subject', '')).render(record), 'utf-8').encode()
        if sender:
            message['From'] = sender
        message['To'] = content.get('to', '')
        message['X-Priority'] = str(content.get('priority', 3))
        message.preamble = message['Subject']
        message.attach(MIMEText(msg, content.get('type', 'plain'), 'utf-8'))
        log.debug("Send mail to {}".format(message['To']))
        self.server = SMTP(host, port, timeout=DEFAULT_TIMEOUT)
        try:
            self.server.sendmail(sender, recipients, message.as_string())
        except Exception as e:
            self.server.close()
            raise
        self.server.close()
