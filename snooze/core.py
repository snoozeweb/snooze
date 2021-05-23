#!/usr/bin/python3.6

from snooze.db.database import Database

from importlib import import_module

from logging import getLogger

log = getLogger('snooze')

from snooze.plugins.core import Abort, Abort_and_write

class Core:
    def __init__(self, conf):
        self.conf = conf
        self.db = Database(conf.get('database', {}))
        self.plugins = []
        self.process_plugins = []
        self.load_plugins()

    def load_plugins(self):
        self.plugins = []
        self.process_plugins = []
        log.debug("Starting to load core plugins")
        for plugin_name in (self.conf.get('core_plugins') or []) + (self.conf.get('process_plugins') or []):
            try:
                log.debug("Attempting to load core plugin {}".format(plugin_name))
                plugin_module = import_module("snooze.plugins.core.{}.plugin".format(plugin_name))
                plugin_class = getattr(plugin_module, plugin_name.capitalize())
            except ModuleNotFoundError:
                log.warning("Module for plugin `{}` not found. Using Basic instead".format(plugin_name))
                plugin_module = import_module("snooze.plugins.core.basic.plugin")
                plugin_class = type(plugin_name.capitalize(), (plugin_module.Plugin,), {})
            except Exception as e:
                log.exception(e)
                log.error("Error for plugin `{}`: {}".format(plugin_name, e))
                continue
            plugin_conf = (self.conf.get(plugin_name) or {})
            plugin_instance = plugin_class(self, plugin_conf)
            self.plugins.append(plugin_instance)
            if (plugin_name in (self.conf.get('process_plugins') or [])):
                log.debug("Detected {} as a process plugin".format(plugin_name))
                self.process_plugins.append(plugin_instance)

    def process_record(self, record):
        record['plugins'] = []
        for plugin in self.process_plugins:
            try:
                log.debug("Executing plugin {} on {}".format(plugin.name, record))
                record['plugins'].append(plugin.name)
                record = plugin.process(record)
            except Abort:
                break
            except Abort_and_write:
                self.db.write('record', record)
                break
            except Exception as e:
                log.exception(e)
                record['exception'] = {
                    'core_plugin': plugin.name,
                    'message': str(e)
                }
                self.db.write('record', record)
                break
        else:
            log.debug("Writing record {}")
            self.db.write('record', record)
