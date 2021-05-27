'''Allow alerts to be sent to snooze server'''
import requests
import validators

import click

class Snooze:
    def __init__(self, server):
        '''Create a new connection to snooze server'''
        if not isinstance(server, str):
            raise TypeError("Parameter `server` must be a string representing a URL.")
        validators.url(server)
        self.server = server

    def alert(self, record):
        '''Send a new alert to snooze'''
        requests.post(f"{self.server}/alert", json=record)
