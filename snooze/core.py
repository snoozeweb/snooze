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
from pathlib import Path
from typing import List
from uuid import uuid4

from dateutil import parser
from opentelemetry.trace import get_tracer, get_current_span

from snooze import __file__ as rootdir
from snooze.db.database import Database, AsyncDatabase, get_database
from snooze.plugins.core import Abort, AbortAndWrite, AbortAndUpdate
from snooze.token import TokenEngine
from snooze.utils.functions import flatten
from snooze.api.socket import WSGISocketServer, admin_api
from snooze.api.tcp import TcpThread
from snooze.api import Api
from snooze.utils import Housekeeper, MQManager
from snooze.utils.stats import Stats
from snooze.utils.config import Config, SNOOZE_CONFIG
from snooze.utils.syncer import Syncer
from snooze.utils.threading import SurvivingThread
from snooze.utils.typing import Record, AuthPayload

log = getLogger('snooze.core')
proclog = getLogger('snooze-process')

tracer = get_tracer('snooze')

MAIN_THREADS = ('housekeeper', 'syncer', 'tcp', 'socket')

class Core:
    '''The main class of snooze, passed to all plugins'''
    def __init__(self, basedir: Path = SNOOZE_CONFIG, allowed_threads: List[str] = MAIN_THREADS):
        self.basedir = basedir
        self.config = Config(basedir)
        core_config = self.config.core
        self.db = get_database(core_config.database)

        self.exit_event = Event()
        self.secrets = self.ensure_secrets()
        self.token_engine = TokenEngine(self.secrets['jwt_private_key'])

        self.threads:  Dict[str, SurvivingThread] = {}
        self.threads['asyncdb'] = AsyncDatabase(self.db, exit_event=self.exit_event)

        self.stats = Stats(self, self.config.general)
        self.stats.bootstrap()

        if 'housekeeper' in allowed_threads:
            self.threads['housekeeper'] = Housekeeper(self.config.housekeeping,
                self.config.core.backup, self.db, self.exit_event)

        self.mq = MQManager(self)

        if 'socket' in allowed_threads and core_config.unix_socket:
            try:
                admin_app = admin_api(self.token_engine)
                self.threads['socket'] = WSGISocketServer(admin_app, core_config.unix_socket, self.exit_event)
            except Exception as err:
                log.warning("Error starting unix socket at %s: %s", core_config.unix_socket, err)

        self.plugins = []
        self.process_plugins = []
        self.bootstrap_db()
        self.load_plugins()

        self.api = Api(self)
        self.api.load_plugin_routes()

        if 'tcp' in allowed_threads:
            tcp_config = core_config.listen_addr, core_config.port, core_config.ssl
            self.threads['tcp'] = TcpThread(tcp_config, self.api.handler, self.exit_event)
        if 'syncer' in allowed_threads:
            self.threads['syncer'] = Syncer(self, self.exit_event)

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
        for plugin_name in self.config.core.process_plugins:
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

    @tracer.start_as_current_span('process_record')
    def process_record(self, record: Record):
        '''Method called when a given record enters the system.
        The method will run the record through all configured plugin,
        except when it receive a specific exception.
        Abort:
            Will abort the processing for a record.
        AbortAndWrite:
            Will abort processing, and write the record in the database.
        AbortAndUpdate:
            Will abort processing, and write the record in the database, but will not
            update the timestamp of the record. This is used mainly by aggregaterule plugin
            for throttling.
        '''
        data = {}
        severity = record.get('severity', 'unknown')
        labels = {
            'source': record.get('source', 'unknown'),
            'environment': record.get('environment', 'unknown'),
            'severity': severity,
        }
        record['ttl'] = int(self.config.housekeeping.record_ttl.total_seconds())
        record['uid'] = str(uuid4())
        proclog.info('New alert received')
        if severity.casefold() in self.config.general.ok_severities:
            record['state'] = 'close'
            log.debug("Detected OK severities: %s, closing alert", severity.casefold())
        else:
            record['state'] = ''
        record['plugins'] = []
        try:
            record['timestamp'] = parser.parse(record['timestamp']).astimezone().strftime("%Y-%m-%dT%H:%M:%S%z")
        except KeyError:
            record['timestamp'] = datetime.now().astimezone().strftime("%Y-%m-%dT%H:%M:%S%z")
        except parser.ParserError as err:
            log.warning(err)
            record['timestamp'] = datetime.now().astimezone().strftime("%Y-%m-%dT%H:%M:%S%z")
        with self.stats.time('process_alert_duration', labels):
            step = 0
            last_plugin = ''
            for plugin in self.process_plugins:
                step += 1
                last_plugin = plugin.name
                plugin_labels = {'environment': labels['environment'], 'plugin': plugin.name}
                with self.stats.time('process_alert_duration_by_plugin', plugin_labels):
                    try:
                        record['plugins'].append(plugin.name)
                        with tracer.start_as_current_span(f"{plugin.name}-plugin"):
                            result = plugin.process(record)
                        if isinstance(result, Abort):
                            data = {'data': {'processed': [record]}}
                            break
                        if isinstance(result, AbortAndWrite):
                            if result.record:
                                record = result.record
                            self.db.replace_one('record', {'uid': record['uid']}, record)
                            data = {'added': [{'uid': record['uid']}]}
                            break
                        if isinstance(result, AbortAndUpdate):
                            if result.record:
                                record = result.record
                            self.db.replace_one('record', {'uid': record['uid']}, record, update_time=False)
                            data = {'updated': [{'uid': record['uid']}]}
                            break
                        record = result
                    except Exception as err:
                        log.exception(err)
                        record['exception'] = {
                            'core_plugin': plugin.name,
                            'message': str(err),
                        }
                        self.db.replace_one('record', {'uid': record['uid']}, record)
                        data = {'added': [{'uid': record['uid']}]}
                        break
            else:
                log.debug("Writing record %s", record)
                self.db.replace_one('record', {'uid': record['uid']}, record)
                data = {'added': [{'uid': record['uid']}]}

        # Adding tracing information
        span = get_current_span()
        span.set_attribute('plugin-step', step)
        span.set_attribute('last-plugin', last_plugin)

        labels = {
            'source': record.get('source', 'unknown'),
            'environment': record.get('environment', 'unknown'),
            'severity': record.get('severity', 'unknown'),
        }
        self.stats.inc('alert_hit', labels)
        proclog.info('Alert processed')
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
                sleep(random() * self.config.core.init_sleep)
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
            self.config.general.refresh()
            self.stats.reload()
            return True
        elif config_file == 'ldap_auth':
            return True
        elif config_file == 'housekeeping':
            self.threads['housekeeper'].reload()
            return True
        elif config_file == 'notifications':
            self.config.notification.refresh()
            return True
        else:
            log.debug("Config file %s not found", config_file)
            return False

    def bootstrap_db(self):
        '''Will attempt to bootstrap the database with default values for the core collections.
        Will not bootstrap anything if it detects a bootstrap happened in the past.
        '''
        if self.config.core.bootstrap_db:
            result = self.db.search('general')
            if result['count'] == 0:
                sleep(random() * self.config.core.init_sleep)
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
                if self.config.core.create_root_user:
                    users = [{"name": "root", "method": "local", "roles": ["admin"], "enabled": True}]
                    self.db.write('user', users)
                    user_passwords = [
                        {"name": "root", "method": "local", "password": sha256("root".encode('utf-8')).hexdigest()},
                    ]
                    self.db.write('user.password', user_passwords)

                self.db.write('profile.general', root)
                self.db.write('general', [{'init_db': True}])

    def init_backup(self):
        '''Create the necessary directory for backups'''
        if self.config.core.backup.enabled:
            try:
                self.config.core.backup.path.mkdir()
            except FileExistsError:
                pass
            except Exception as err:
                log.exception(err)
