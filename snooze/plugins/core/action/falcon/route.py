#!/usr/bin/python
from logging import getLogger
log = getLogger('snooze.api')

from snooze.plugins.core.basic.falcon.route import Route
from snooze.api.falcon import authorize

class ActionRoute(Route):
    @authorize
    def on_post(self, req, resp):
        for req_media in req.media:
            self.inject_pprint(req_media)
        super(ActionRoute, self).on_post(req, resp)

    @authorize
    def on_put(self, req, resp):
        for req_media in req.media:
            self.inject_pprint(req_media)
        super(ActionRoute, self).on_put(req, resp)

    def inject_pprint(self, media):
        action = media.get('action', [])
        plugin_name = action.get('selected')
        content = action.get('subcontent')
        plugin = self.api.core.get_action_plugin(plugin_name)
        if plugin:
            media['pprint'] = plugin.pprint(content)
        else:
            media['pprint'] = plugin_name
        media['icon'] = plugin.get_icon()
