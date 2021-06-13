#!/usr/bin/python3.6

from snooze.db.database import Database

from importlib import import_module

from logging import getLogger

log = getLogger('snooze')

from snooze.plugins.core import Abort, Abort_and_write
from snooze.utils import config, Housekeeper, Stats

class Core:
    def __init__(self, conf):
        self.conf = conf
        self.db = Database(conf.get('database', {}))
        self.general_conf = config('general')
        self.housekeeper = Housekeeper(self)
        self.cluster = None
        self.plugins = []
        self.process_plugins = []
        self.stats = Stats(conf.get('stats'))
        self.secrets = config('secrets')
        self.stats.init('process_record_duration')
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
        source = record.get('source', 'unknown')
        record['ttl'] = self.housekeeper.conf.get('record_ttl', 86400)
        record['plugins'] = []
        with self.stats.time('process_record_duration', {'source': source}):
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

    def reload_conf(self, config_file):
        log.debug("Reload config file '{}'".format(config_file))
        if config_file == 'general':
            self.general_conf = config('general')
            return True
        if config_file == 'ldap_auth':
            return True
        elif config_file == 'housekeeping':
            return self.housekeeper.reload()
        elif config_file == 'stats':
            return self.stats.reload()
        else:
            log.debug("Config file {} not found".format(config_file))
            return False
