#!/usr/bin/python3.6

from snooze.db.database import Database

from importlib import import_module

from logging import getLogger

log = getLogger('snooze')

from os import listdir
from os.path import dirname, isdir, join as joindir
from snooze import __file__ as rootdir
from snooze.plugins.core import Abort, Abort_and_write
from snooze.utils import config, Housekeeper, Stats
from hashlib import sha256
from dateutil import parser
from datetime import datetime

class Core:
    def __init__(self, conf):
        self.conf = conf
        self.db = Database(conf.get('database', {}))
        self.general_conf = config('general')
        self.housekeeper = Housekeeper(self)
        self.cluster = None
        self.plugins = []
        self.process_plugins = []
        self.stats = Stats(self)
        self.secrets = config('secrets')
        self.stats.init('process_record_duration', 'summary', 'snooze_record_process_duration', 'Average time spend processing a record', ['source'])
        self.stats.init('notification_sent', 'counter', 'snooze_notification_sent', 'Counter of notification sent', ['name', 'action'])
        self.stats.init('notification_error', 'counter', 'snooze_notification_error', 'Counter of notification that failed', ['name', 'action'])
        self.bootstrap_db()
        self.load_plugins()

    def load_plugins(self):
        self.plugins = []
        self.process_plugins = []
        log.debug("Starting to load core plugins")
        plugins_path = joindir(dirname(rootdir), 'plugins', 'core')
        for plugin_name in listdir(plugins_path):
            if (not isdir(plugins_path + '/' + plugin_name)) or plugin_name == 'basic' or plugin_name.startswith('_'):
                continue
            try:
                log.debug("Attempting to load core plugin {}".format(plugin_name))
                plugin_module = import_module("snooze.plugins.core.{}.plugin".format(plugin_name))
                plugin_class = getattr(plugin_module, plugin_name.capitalize())
            except ModuleNotFoundError:
                log.debug("Module for plugin `{}` not found. Using Basic instead".format(plugin_name))
                plugin_module = import_module("snooze.plugins.core.basic.plugin")
                plugin_class = type(plugin_name.capitalize(), (plugin_module.Plugin,), {})
            except Exception as e:
                log.exception(e)
                log.error("Error init core plugin `{}`: {}".format(plugin_name, e))
                continue
            plugin_instance = plugin_class(self)
            self.plugins.append(plugin_instance)
            if (plugin_name in (self.conf.get('process_plugins') or [])):
                log.debug("Detected {} as a process plugin".format(plugin_name))
                self.process_plugins.append(plugin_instance)
        for plugin in self.plugins:
            try:
                plugin.post_init()
            except Exception as e:
                log.exception(e)
                log.error("Error post init core plugin `{}`: {}".format(plugin_name, e))
                continue
        log.debug("List of loaded core plugins: {}".format([plugin.name for plugin in self.plugins]))

    def process_record(self, record):
        source = record.get('source', 'unknown')
        record['ttl'] = self.housekeeper.conf.get('record_ttl', 86400)
        record['state'] = ''
        record['plugins'] = []
        try:
            record['timestamp'] = parser.parse(record['timestamp']).astimezone().strftime("%Y-%m-%dT%H:%M:%S%z")
        except Exception as e:
            log.warning(e)
            record['timestamp'] = datetime.now().astimezone().strftime("%Y-%m-%dT%H:%M:%S%z")
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
                log.debug("Writing record {}".format(record))
                self.db.write('record', record)

    def get_core_plugin(self, plugin_name):
        return next(iter([plug for plug in self.plugins if plug.name == plugin_name]), None)

    def reload_conf(self, config_file):
        log.debug("Reload config file '{}'".format(config_file))
        if config_file == 'general':
            self.general_conf = config('general')
            self.stats.reload()
            return True
        elif config_file == 'ldap_auth':
            return True
        elif config_file == 'housekeeping':
            self.housekeeper.reload()
            return True
        else:
            log.debug("Config file {} not found".format(config_file))
            return False

    def bootstrap_db(self):
        if self.conf.get('bootstrap_db', False):
            result = self.db.search('general')
            if result['count'] == 0:
                log.debug("First time starting Snooze with self database. Let us configure it...")
                aggregate_rules = [{"fields": [ "host", "message" ], "snooze_user": "root" , "name": "Host and Message", "condition": [], "throttle": 900 }]
                self.db.write('aggregaterule', aggregate_rules)
                roles = [{"name": "admin", "permissions": [ "rw_all" ], "snooze_user": "root"}, {"name": "user", "permissions": [ "ro_all" ], "snooze_user": "root"}]
                self.db.write('role', roles)
                if self.conf.get('create_root_user', False):
                    users = [{"name": "root", "method": "local", "roles": ["admin"], "enabled": True}]
                    self.db.write('user', users)
                    user_passwords = [{"name": "root", "method": "local", "password": sha256("root".encode('utf-8')).hexdigest()}]
                    self.db.write('user.password', user_passwords)
                self.db.write('general', [{'init_db': True}])

