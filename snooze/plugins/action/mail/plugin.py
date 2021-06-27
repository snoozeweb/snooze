#!/usr/bin/python36

from smtplib import SMTP
from jinja2 import Template

from snooze.plugins.action import Action

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

class Mail(Action):
    def __init__(self, core):
        super().__init__(core)
        #self.message_template = self.notification.get('message_template', DEFAULT_MESSAGE_TEMPLATE)
        #host = notification.get('host', 'localhost')
        #port = notification.get('port', '25')
        #self.server = SMTP(self.host, self.port)
        #self.recipient = notification.get('recipient')
        #self.sender = notification.get('sender')

    def pprint(self, content):
        output  =  'mailto: ' + content.get('to', '').replace(',', '\nmailto: ')
        return output

    def send(self, record):
        #message = Template(message_template).render(record)
        #self.server.sendmail(self.sender, self.recipient, message)
        pass
