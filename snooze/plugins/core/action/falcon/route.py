#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''Custom falcon routes for the action plugin'''

from logging import getLogger

import falcon

from snooze.api.base import BasicRoute
from snooze.api.falcon import authorize
from snooze.plugins.core.basic.falcon.route import Route

log = getLogger('snooze.api')

class ActionRoute(Route):
    '''Overriding post/put to incude pre-computed values for the web
    interface'''
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
    '''A route to list the action plugin types (script, webhook, mail, etc)'''
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
                log.error("Could not find action plugin for request %s", req.params)
            for plugin in loaded_plugins:
                plugin_metadata = plugin.get_metadata()
                if plugin_metadata.get('action_form'):
                    log.debug("Retrieving action %s metadata", plugin.name)
                    plugins.append(plugin_metadata)
            log.debug("List of actions: %d elements", len(plugins))
            resp.content_type = falcon.MEDIA_JSON
            resp.status = falcon.HTTP_200
            resp.media = {
                'data': plugins,
            }
        except Exception as err:
            log.exception(err)
            resp.status = falcon.HTTP_503
