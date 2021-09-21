'''Module for managing the snooze server core'''

from datetime import datetime
from hashlib import sha256
from importlib import import_module
from logging import getLogger
from os import listdir
from os.path import dirname, isdir, join as joindir
from secrets import token_urlsafe
from threading import Event

from dateutil import parser

from snooze import __file__ as rootdir
from snooze.db.database import Database
from snooze.plugins.core import Abort, Abort_and_write, Abort_and_update
from snooze.token import TokenEngine
from snooze.utils import config, Housekeeper, Stats

log = getLogger('snooze')

class Core:
    def __init__(self, conf):
        self.conf = conf
        self.db = Database(conf.get('database', {}))
        self.general_conf = config('general')
        self.housekeeper = Housekeeper(self)
        self.cluster = None
        self.exit_button = Event()
        self.plugins = []
        self.process_plugins = []
        self.stats = Stats(self)
        self.stats.init('process_record_duration', 'summary', 'snooze_record_process_duration', 'Average time spend processing a record', ['source'])
        self.stats.init('notification_sent', 'counter', 'snooze_notification_sent', 'Counter of notification sent', ['name', 'action'])
        self.stats.init('notification_error', 'counter', 'snooze_notification_error', 'Counter of notification that failed', ['name', 'action'])
        self.bootstrap_db()
        self.secrets = self.ensure_secrets()
        self.token_engine = TokenEngine(self.secrets['jwt_private_key'])
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
        for plugin_name in self.conf.get('process_plugins', []):
            for plugin in self.plugins:
                if plugin_name == plugin.name:
                    log.debug("Detected {} as a process plugin".format(plugin_name))
                    self.process_plugins.append(plugin)
                    break
        for plugin in self.plugins:
            try:
                plugin.post_init()
            except Exception as e:
                log.exception(e)
                log.error("Error post init core plugin `{}`: {}".format(plugin.name, e))
                continue
        log.debug("List of loaded core plugins: {}".format([plugin.name for plugin in self.plugins]))
        log.debug("List of loaded process plugins: {}".format([plugin.name for plugin in self.process_plugins]))

    def process_record(self, record):
        data = {}
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
                    data = {'data': {'rejected': [record]}}
                    break
                except Abort_and_write as e:
                    data = self.db.write('record', e.record or record)
                    break
                except Abort_and_update as e:
                    data = self.db.write('record', e.record or record, update_time=False)
                    break
                except Exception as e:
                    log.exception(e)
                    record['exception'] = {
                        'core_plugin': plugin.name,
                        'message': str(e)
                    }
                    data = self.db.write('record', record)
                    break
            else:
                log.debug("Writing record {}".format(record))
                data = self.db.write('record', record)
        return data

    def get_core_plugin(self, plugin_name):
        return next(iter([plug for plug in self.plugins if plug.name == plugin_name]), None)

    def get_secrets(self):
        '''Return a dict of secrets stored in the database'''
        results = self.db.search('secrets', ['=', 'type', 'secret'])
        if results.get('count') > 0:
            secrets = {}
            for data in results['data']:
                name = data.get('name')
                value = data.get('value')
                if name:
                    secrets[name] = value
            return secrets
        else:
            return {}

    def ensure_secrets(self):
        '''Bootstrap the secrets if not present and store them in the database'''
        should = {
            'jwt_private_key': lambda: token_urlsafe(128),
            'reload_token': lambda: token_urlsafe(32),
        }
        actual = self.get_secrets()
        towrite = []
        for name, method in should.items():
            if name not in actual:
                secret = method()
                towrite.append({'type': 'secret', 'name': name, 'value': secret})
        if towrite:
            self.db.write('secrets', towrite)
        return self.get_secrets()

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
