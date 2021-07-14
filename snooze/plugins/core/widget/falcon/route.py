'''Routes for widget'''

import copy
import falcon
from bson.json_util import loads, dumps

from logging import getLogger
log = getLogger('snooze.widget')

from snooze.api.falcon import authorize
from snooze.plugins.core.basic.falcon.route import Route
from snooze.utils.config import get_metadata

class WidgetPluginRoute(Route):
    '''A route to list all widget plugins (to instantiate widgets)'''
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.name = 'widget_plugins'

    def list_widget_plugins(self):
        '''Return the list of loaded Widget plugins'''
        widgets = getattr(self.core, 'widget_plugins', [])
        # Remove non-serializable keys
        for widget in widgets:
            widget.pop('python_instance', None)
        return widgets

    def on_get(self, req, resp):
        log.debug("Listing widgets")
        widgets = self.list_widget_plugins()
        resp.content_type = falcon.MEDIA_JSON
        resp.status = falcon.HTTP_OK
        resp.media = {
            'data': widgets,
        }

class WidgetRoute(Route):
    @authorize
    def on_get(self, req, resp, **kwargs):
        super(WidgetRoute, self).on_get(req, resp, **kwargs)
        widgets = loads(resp.body).get('data')
        for widget in widgets:
            matching_plugins = [
                plugin for plugin in self.core.widget_plugins
                if plugin.get('name') \
                and plugin.get('name') == widget.get('widget', {}).get('selected')
            ]
            log.debug("Plugins: %s", matching_plugins)
            if matching_plugins:
                plugin = copy.deepcopy(matching_plugins[0])
                plugin.pop('python_instance', None)
                widget['form'] = plugin.get('form')
                widget['plugin_name'] = plugin.get('name')
                widget['vue_component'] = plugin.get('vue_component')
        resp.body = dumps({'data': widgets})
