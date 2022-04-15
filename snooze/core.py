#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''Module for managing the snooze server core'''

from random import random
from datetime import datetime
from time import sleep
from hashlib import sha256
from importlib import import_module
from logging import getLogger
from os import listdir, mkdir
from os.path import dirname, isdir, join as joindir
from secrets import token_urlsafe
from threading import Event
from typing import Dict, Optional
from pkg_resources import iter_entry_points

from dateutil import parser

from snooze import __file__ as rootdir
from snooze.api.base import Api
from snooze.db.database import Database
from snooze.plugins.core import Abort, Abort_and_write, Abort_and_update
from snooze.token import TokenEngine
from snooze.utils import config, Housekeeper, Stats, MQManager
from snooze.utils.functions import flatten
from snooze.utils.typing import Config, Record
from snooze.api.socket import WSGISocketServer, admin_api
from snooze.api.tcp import TcpThread
from snooze.utils.cluster import Cluster
from snooze.utils.threading import SurvivingThread

log = getLogger('snooze')

class Core:
    '''The main class of snooze, passed to all plugins'''
    def __init__(self, conf: Config):
        self.conf = conf
        self.db = Database(conf.get('database', {}))
        self.general_conf = config('general')
        self.notif_conf = config('notifications')
        self.ok_severities = list(map(lambda x: x.casefold(), flatten([self.general_conf.get('ok_severities', [])])))
        self.init_backup()

        self.stats = Stats(self)
        self.stats.init('process_alert_duration', 'summary', 'snooze_process_alert_duration',
            'Average time spend processing a alert', ['source', 'environment', 'severity'])
        self.stats.init('alert_hit', 'counter', 'snooze_alert_hit',
            'Counter of received alerts', ['source', 'environment', 'severity'])
        self.stats.init('alert_snoozed', 'counter', 'snooze_alert_snoozed', 'Counter of snoozed alerts', ['name'])
        self.stats.init('alert_throttled', 'counter', 'snooze_alert_throttled', 'Counter of throttled alerts', ['name'])
        self.stats.init('alert_closed', 'counter', 'snooze_alert_closed', 'Counter of received closed alerts', ['name'])
        self.stats.init('notification_sent', 'counter', 'snooze_notification_sent',
            'Counter of notification sent', ['name'])
        self.stats.init('action_success', 'counter', 'snooze_action_success',
            'Counter of action that succeeded', ['name'])
        self.stats.init('action_error', 'counter', 'snooze_action_error', 'Counter of action that failed', ['name'])

        self.exit_event = Event()
        self.secrets = self.ensure_secrets()
        self.token_engine = TokenEngine(self.secrets['jwt_private_key'])

        self.threads:  Dict[str, SurvivingThread] = {}
        self.threads['housekeeper'] = Housekeeper(self)
        self.threads['cluster'] = Cluster(self)
        self.mq = MQManager(self)

        unix_socket = self.conf.get('unix_socket')
        if unix_socket:
            try:
                admin_app = admin_api(self.token_engine)
                self.threads['socket'] = WSGISocketServer(admin_app, unix_socket, self.exit_event)
            except Exception as err:
                log.warning("Error starting unix socket at %s: %s", unix_socket, err)

        self.plugins = []
        self.process_plugins = []
        self.bootstrap_db()
        self.load_plugins()

        self.api = Api(self)
        self.api.load_plugin_routes()
        self.threads['tcp'] = TcpThread(self.conf, self.api.handler, self.exit_event)

    def load_plugins(self):
        '''Load the plugins from the configuration'''
        self.plugins = []
        self.process_plugins = []
        log.debug("Starting to load core plugins")
        plugins_path = joindir(dirname(rootdir), 'plugins', 'core')
        for entry_point in iter_entry_points('snooze.plugins.core'):
            log.debug("External core plugin '%s' detected", entry_point.name)
            plugin_class = entry_point.load()
            plugin_instance = plugin_class(self)
            self.plugins.append(plugin_instance)
        for plugin_name in listdir(plugins_path):
            if (not isdir(plugins_path + '/' + plugin_name)) or plugin_name == 'basic' or plugin_name.startswith('_'):
                continue
            try:
                log.debug("Attempting to load core plugin %s", plugin_name)
                plugin_module = import_module(f"snooze.plugins.core.{plugin_name}.plugin")
                plugin_class = getattr(plugin_module, plugin_name.capitalize())
            except ModuleNotFoundError:
                log.debug("Module for plugin `%s` not found. Using Basic instead", plugin_name)
                plugin_module = import_module("snooze.plugins.core.basic.plugin")
                plugin_class = type(plugin_name.capitalize(), (plugin_module.Plugin,), {})
            except Exception as err:
                log.exception(err)
                log.error("Error init core plugin `%s`: %s",plugin_name, err)
                continue
            plugin_instance = plugin_class(self)
            self.plugins.append(plugin_instance)
        for plugin_name in self.conf.get('process_plugins', []):
            for plugin in self.plugins:
                if plugin_name == plugin.name:
                    log.debug("Detected %s as a process plugin", plugin_name)
                    self.process_plugins.append(plugin)
                    break
        for plugin in self.plugins:
            try:
                plugin.post_init()
            except Exception as err:
                log.exception(err)
                log.error("Error post init core plugin `%s`: %s", plugin.name, err)
                continue
        log.debug("List of loaded core plugins: %s", [plugin.name for plugin in self.plugins])
        log.debug("List of loaded process plugins: %s", [plugin.name for plugin in self.process_plugins])

    def process_record(self, record: Record):
        '''Method called when a given record enters the system.
        The method will run the record through all configured plugin,
        except when it receive a specific exception.
        Abort:
            Will abort the processing for a record.
        Abort_and_write:
            Will abort processing, and write the record in the database.
        Abort_and_update:
            Will abort processing, and write the record in the database, but will not
            update the timestamp of the record. This is used mainly by aggregaterule plugin
            for throttling.
        '''
        data = {}
        source = record.get('source', 'unknown')
        environment = record.get('environment', 'unknown')
        severity = record.get('severity', 'unknown')
        record['ttl'] = self.threads['housekeeper'].conf.get('record_ttl', 86400)
        if severity.casefold() in self.ok_severities:
            record['state'] = 'close'
        else:
            record['state'] = ''
        record['plugins'] = []
        try:
            record['timestamp'] = parser.parse(record['timestamp']).astimezone().strftime("%Y-%m-%dT%H:%M:%S%z")
        except Exception as err:
            log.warning(err)
            record['timestamp'] = datetime.now().astimezone().strftime("%Y-%m-%dT%H:%M:%S%z")
        with self.stats.time('process_alert_duration', {'source': source, 'environment': environment, 'severity': severity}):
            for plugin in self.process_plugins:
                try:
                    log.debug("Executing plugin %s on record %s", plugin.name, record.get('hash', ''))
                    record['plugins'].append(plugin.name)
                    record = plugin.process(record)
                except Abort:
                    data = {'data': {'processed': [record]}}
                    break
                except Abort_and_write as abort:
                    data = self.db.write('record', abort.record or record, duplicate_policy='replace')
                    break
                except Abort_and_update as abort:
                    data = self.db.write('record', abort.record or record, update_time=False, duplicate_policy='replace')
                    break
                except Exception as err:
                    log.exception(err)
                    record['exception'] = {
                        'core_plugin': plugin.name,
                        'message': str(err),
                    }
                    data = self.db.write('record', record, duplicate_policy='replace')
                    break
            else:
                log.debug("Writing record %s", record)
                data = self.db.write('record', record, duplicate_policy='replace')
        environment = record.get('environment', 'unknown')
        severity = record.get('severity', 'unknown')
        self.stats.inc('alert_hit', {'source': source, 'environment': environment, 'severity': severity})
        return data

    def get_core_plugin(self, plugin_name: str) -> Optional['Plugin']:
        '''Return a core plugin object by name'''
        return next(iter([plug for plug in self.plugins if plug.name == plugin_name]), None)

    def get_secrets(self) -> dict:
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
                sleep(random()*self.conf.get('init_sleep', 5))
                actual = self.get_secrets()
                break
        for name, method in should.items():
            if name not in actual:
                secret = method()
                towrite.append({'type': 'secret', 'name': name, 'value': secret})
        if towrite:
            self.db.write('secrets', towrite)
        return self.get_secrets()

    def reload_conf(self, config_file):
        '''Reload the configuration file'''
        log.debug("Reload config file '%s'", config_file)
        if config_file == 'general':
            self.general_conf = config('general')
            ok_severities = self.general_conf.get('ok_severities', [])
            self.ok_severities = [x.casefold() for x in flatten([ok_severities])]
            self.stats.reload()
            return True
        elif config_file == 'ldap_auth':
            return True
        elif config_file == 'housekeeping':
            self.threads['housekeeper'].reload()
            return True
        elif config_file == 'notifications':
            self.notif_conf = config('notifications')
            return True
        else:
            log.debug("Config file %s not found", config_file)
            return False

    def bootstrap_db(self):
        '''Will attempt to bootstrap the database with default values for the core collections.
        Will not bootstrap anything if it detects a bootstrap happened in the past.
        '''
        if self.conf.get('bootstrap_db', False):
            result = self.db.search('general')
            if result['count'] == 0:
                sleep(random()*self.conf.get('init_sleep', 10))
                result = self.db.search('general')
            if result['count'] == 0:
                log.debug("First time starting Snooze with self database. Let us configure it...")
                root = {'name': 'root', 'method': 'root'}
                aggregate_rules = [
                    {
                        "fields": [ "host", "message" ],
                        "snooze_user": root,
                        "name": "Host and Message",
                        "condition": [],
                        "throttle": 900,
                    },
                ]
                self.db.write('aggregaterule', aggregate_rules)
                roles = [
                    {
                        "name": "admin",
                        "permissions": [ "rw_all" ],
                        "snooze_user": root,
                    },
                    {
                        "name": "user",
                        "permissions": [ "ro_all" ],
                        "snooze_user": root,
                    },
                ]
                self.db.write('role', roles)
                if self.conf.get('create_root_user', False):
                    users = [{"name": "root", "method": "local", "roles": ["admin"], "enabled": True}]
                    self.db.write('user', users)
                    user_passwords = [
                        {"name": "root", "method": "local", "password": sha256("root".encode('utf-8')).hexdigest()},
                    ]
                    self.db.write('user.password', user_passwords)
                self.db.write('general', [{'init_db': True}])

    def init_backup(self):
        '''Create the necessary directory for backups'''
        if self.conf.get('backup', {}).get('enabled', True):
            try:
                mkdir(self.conf.get('backup', {}).get('path', './backups'))
            except FileExistsError:
                pass
            except Exception as err:
                log.exception(err)
