#!/usr/bin/python3.6

from snooze.plugins.core import Plugin

import logging
from logging import getLogger
log = getLogger('snooze.user')

class Patlite(Plugin):
    def pprint(self, widget_name, options):
        '''
        Determine the pretty print for the Patlite action plugin.
        This is how the information will be printed on the web interface
        to represent this action.
        '''
        host = options.get('host')
        port = options.get('port')
        output = host + ':' + str(port)
        return output
