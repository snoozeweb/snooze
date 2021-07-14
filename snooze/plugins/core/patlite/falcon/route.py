'''Routes for Patlite widget support'''

import falcon

from snooze.plugins.core.basic.falcon.route import Route
from snooze.api.falcon import authorize

from snooze.utils.patlite import Patlite, PatliteError

from logging import getLogger
log = getLogger('snooze.patlite')

class PatliteStatusRoute(Route):
    @authorize
    def on_get(self, req, resp, name):
        log.debug("Trying to find Patlite %s", name)
        results = self.search('patlite', ['=', 'name', name], 1, 1)
        if results.get('data'):
            patlite = results.get('data')[0]
        else:
            raise falcon.HTTPBadRequest(
                title="Patlite not found",
                description="Patlite '%s' was not found in the database" % name,
            )

        host = patlite.get('host')
        port = patlite.get('port')
        with Patlite(host, port) as api:
            try:
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
    def on_post(self, req, resp, name):
        log.debug("Trying to find Patlite %s", name)
        results = self.search('patlite', ['=', 'name', name], 1, 1)
        if results.get('data'):
            patlite = results.get('data')[0]
        else:
            raise falcon.HTTPBadRequest(
                title="Patlite not found",
                description="Patlite %s was not found in the database" % name,
            )

        host = patlite.get('host')
        port = patlite.get('port')
        with Patlite(host, port) as api:
            try:
                api.reset()
            except PatliteError as err:
                raise falcon.HTTPInternalServerError(
                    title="Error querying Patlite",
                    description=str(err),
                )

