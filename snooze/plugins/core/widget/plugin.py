'''A plugin for managing web interface widgets'''

from pkgutil import iter_modules
from importlib import import_module

from snooze.plugins.core import Plugin
import snooze.plugins.core

from snooze.utils.config import get_metadata

from logging import getLogger
log = getLogger('snooze.widgets')

class WidgetBase():
    pass

class Widget(Plugin):
    '''
    The Widget core plugin
    Features:
    * Load widget plugins (plugins that defined a Widget class)
    '''
    def __init__(self, core, conf):
        super().__init__(core, conf)
        self.core.widget_plugins = self.load_plugins()

    def list_widget_plugins(self):
        '''Return the list of loaded Widget plugins'''
        return getattr(self.core, 'widget_plugins', [])

    @staticmethod
    def load_widget_plugin(namespace, plugin_class):
        '''Load the class of a widget plugin and return an instance'''

        try:
            module = import_module(namespace)
        except Exception as err:
            log.error("Error while loading module %s: %s", namespace, err)
            return None

        try:
            plugin_class_object = getattr(module, plugin_class)
        except Exception as err:
            log.error("Error while loading %s from %s: %s", plugin_class, module, err)
            return None

        try:
            plugin_instance = plugin_class_object()
        except Exception as err:
            log.error("Error while instantiating %s.%s: %s", namespace, plugin_class, err)

    @staticmethod
    def get_core_plugin_namespaces():
        '''List all the core plugin namespaces'''
        plugin_module = snooze.plugins.core
        plugin_namespaces = (
            name
            for finder, name, ispkg
            in iter_modules(plugin_module.__path__, plugin_module.__name__ + '.')
        )
        return plugin_namespaces

    def load_plugins(self):
        '''Load the plugins related to Widget'''
        log.debug("Starting to load widget plugins")

        plugin_namespaces = self.get_core_plugin_namespaces()

        widget_plugins = []

        for namespace in plugin_namespaces:

            plugin_name = namespace.split('.')[-1]
            metadata = get_metadata(plugin_name)
            widgets = metadata.get('widgets', {})

            for name, widget in widgets.items():
                python_class = widget.get('python_class')
                if python_class:
                    widget['python_instance'] = self.load_widget_plugin(namespace, python_class)

                widget_plugins.append({'name': name, **widget})

        return widget_plugins

    def reload_data(self, sync = False):
        super().reload_data()
        notification_plugin = self.core.get_core_plugin('notification')
        if notification_plugin:
            notification_plugin.reload_data()
        if sync and self.core.cluster:
            self.core.cluster.reload_plugin(self.name)
