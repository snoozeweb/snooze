'''Routes for Patlite widget support'''

import falcon

from snooze.plugins.core.basic.falcon.route import Route
from snooze.api.falcon import authorize

from snooze.utils.patlite import Patlite, PatliteError

from logging import getLogger
log = getLogger('snooze.patlite')

class PatliteStatusRoute(Route):
    @authorize
    def on_get(self, req, resp):
        host = req.params.get('host')
        port = req.params.get('port')
        try:
            with Patlite(host, int(port)) as api:
                resp.media = api.get_state().mystate
                resp.status = falcon.HTTP_OK
                return
        except Exception as err:
            raise falcon.HTTPInternalServerError(
                title="Error querying Patlite",
                description=str(err)
            )

class PatliteResetRoute(Route):
    @authorize
    def on_post(self, req, resp):
        host = req.params.get('host')
        port = req.params.get('port')
        try:
            with Patlite(host, int(port)) as api:
                api.reset()
        except Exception as err:
            raise falcon.HTTPInternalServerError(
                title="Error querying Patlite",
                description=str(err),
            )

