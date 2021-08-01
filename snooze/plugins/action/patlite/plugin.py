'''Action plugin to send alerts to a Patlite'''

from snooze.utils.patlite import Patlite as PatliteAPI, State
from snooze.plugins.action import Action
from logging import getLogger
log = getLogger('snooze.action.patlite')

class Patlite(Action):
    def pprint(self, options):
        '''
        Determine the pretty print for the Patlite action plugin.
        This is how the information will be printed on the web interface
        to represent this action.
        '''
        host = options.get('host')
        port = options.get('port')
        lights = [k+': '+v for k,v in options.get('lights').items() if v != 'off']
        output = host + ' @ ' + " - ".join(lights)
        return output

    def send(self, record, options):
        '''
        Determine the action that will be taken when this action is invoked.
        It will set the lights and alarm of the Patlite.
        '''
        lights = options.get('lights')
        host = options.get('host')
        port = int(options.get('port'))
        log.debug("Will execute action patlite `{}:{}` with lights `{}`".format(host, port, lights))
        with PatliteAPI(host, port=port) as patlite:
            patlite.set_full_state(State(**lights))
