#!/usr/bin/python
import falcon
from logging import getLogger
log = getLogger('snooze.api')

from snooze.plugins.core.basic.falcon.route import Route
from snooze.api.base import BasicRoute
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
        plugin = self.api.core.get_core_plugin(plugin_name)
        if plugin:
            media['pprint'] = plugin.pprint(content)
        else:
            media['pprint'] = plugin_name
        media['icon'] = plugin.get_icon()

class ActionPluginRoute(BasicRoute):
    @authorize
    def on_get(self, req, resp, action=None):
        log.debug("Listing actions")
        plugin_name = req.params.get('action') or action
        try:
            plugins = []
            loaded_plugins = self.api.core.plugins
            if plugin_name:
                loaded_plugins = [self.api.core.get_core_plugin(plugin_name)]
            else:
                log.error("Could not find action plugin for request {}".format(req.params))
            for plugin in loaded_plugins:
                plugin_metadata = plugin.get_metadata()
                if plugin_metadata.get('action_form'):
                    log.debug("Retrieving action {} metadata: {}".format(plugin.name, plugin_metadata))
                    plugins.append(plugin_metadata)
            log.debug("List of actions: {}".format(plugins))
            resp.content_type = falcon.MEDIA_JSON
            resp.status = falcon.HTTP_200
            resp.media = {
                'data': plugins,
            }
        except Exception as e:
            log.exception(e)
            resp.status = falcon.HTTP_503
