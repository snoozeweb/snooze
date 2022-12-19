#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

import os
import inspect
from unittest.mock import patch
from pathlib import Path

from snooze.utils.config import *

class TestConfig:
    def test_empty(self, tmp_path):
        config = Config(tmp_path)
        assert isinstance(config.core, CoreConfig)
        assert isinstance(config.general, GeneralConfig)
        assert isinstance(config.notifications, NotificationConfig)
        assert isinstance(config.ldap_auth, LdapConfig)

class TestCoreConfig:
    def test_empty(self, tmp_path):
        config = CoreConfig(tmp_path)
        assert config.listen_addr == IPv4Address('0.0.0.0')
        assert config.port == 5200

    def test_read(self, tmp_path):

        core_path = tmp_path / 'core.yaml'
        data = inspect.cleandoc('''---
        listen_addr: '0.0.0.0'
        port: '5200'
        bootstrap_db: true
        create_root_user: true
        unix_socket: /var/run/snooze/server-test.socket
        no_login: false
        audit_excluded_paths: ['/api/patlite', '/metrics', '/web']
        ssl:
          enabled: true
          certfile: '/etc/pki/tls/certs/snooze.crt'
          keyfile: '/etc/pki/tls/private/snooze.key'
        web:
          enabled: true
          path: /opt/snooze/web
        process_plugins: [rule, aggregaterule, snooze, notification]
        database:
          type: mongo
        ''')
        core_path.write_text(data)

        config = CoreConfig(tmp_path)
        assert config.listen_addr == IPv4Address('0.0.0.0')
        assert config.port == 5200
        assert config.bootstrap_db == True

class TestDatabaseConfig:
    def test_mongo(self, tmp_path):
        data = inspect.cleandoc('''---
        database:
            type: mongo
            host:
                - host01
                - host02
                - host03
            port: 27017
            username: snooze
            password: secret123
            authSource: snooze
            replicaSet: rs0
            tls: true
            tlsCAFile: '/etc/pki/tls/cert.pem'
        ''')
        core_path = tmp_path / 'core.yaml'
        core_path.write_text(data)

        config = CoreConfig(tmp_path)
        assert config.database.type == 'mongo'
        assert config.database.host == ['host01', 'host02', 'host03']

    def test_file(self, tmp_path):
        config = CoreConfig(tmp_path)
        assert config.database.type == 'file'

class TestHousekeeperConfig:
    def test_empty(self, tmp_path):
        config = HousekeeperConfig(tmp_path)
        assert config

class TestGeneralConfig:
    def test_empty(self, tmp_path):
        config = GeneralConfig(tmp_path)
        assert config

class TestNotificationConfig:
    def test_empty(self, tmp_path):
        config = NotificationConfig(tmp_path)
        assert config

class TestLdapConfig:
    def test_disabled(self, tmp_path):
        config = LdapConfig(tmp_path)
        assert config
        assert config.enabled == False
        assert config.bind_dn == None
        assert config.bind_password == None

    def test_update(self, tmp_path):
        config = LdapConfig(tmp_path)
        myupdate = {
            'enabled': True,
            'host': 'ldap.example.com',
            'bind_dn': 'cn=myuser,ou=users,dc=example,dc=com',
            'base_dn': 'ou=users,dc=example,dc=com',
            'bind_password': 'my-secret-password123',
            'user_filter': '()'
        }
        config.update(myupdate)
        assert config.enabled == True
        assert config.host == 'ldap.example.com'

        # Checking file content
        path = tmp_path / 'ldap_auth.yaml'
        text = path.read_text(encoding='utf-8')
        data = yaml.safe_load(text)
        assert data['enabled'] == True
        assert data['host'] == 'ldap.example.com'

    def test_excluded_update(self, tmp_path):
        text = inspect.cleandoc('''---
        enabled: true
        host: ldap.example.com
        bind_dn: cn=myuser,ou=users,dc=example,dc=com
        base_dn: ou=users,dc=example,dc=com
        bind_password: my-secret-password123
        user_filter: '()'
        ''')
        ldap_auth = tmp_path / 'ldap_auth.yaml'
        ldap_auth.write_text(text, encoding='utf-8')
        config = LdapConfig(tmp_path)
        assert config.bind_password == 'my-secret-password123'

        # Attempting a falsy update of the excluded field
        config.bind_password = ''
        assert config.bind_password == 'my-secret-password123'

class TestMetadataConfig:
    def test_all_plugins(self):
        metadata_files = SNOOZE_PLUGIN_PATH.glob('*/metadata.yaml')
        plugins = [path.parent.name for path in metadata_files]
        assert plugins
        metadata = {}
        for plugin in plugins:
            metadata[plugin] = MetadataConfig(plugin, SNOOZE_PLUGIN_PATH / plugin)

        assert metadata['audit'].name == 'Audit'
        assert metadata['snooze'].name == 'Snooze'

class TestEnvSettings:

    @patch.dict(os.environ, {'SNOOZE_SERVER_GENERAL_METRICS_ENABLED': '0'}, clear=True)
    def test_general_config(self, tmp_path):
        settings = GeneralConfig(tmp_path)
        assert settings.metrics_enabled == False

    @patch.dict(os.environ, {'SNOOZE_SERVER_CORE_SSL_CERTFILE': '/etc/pki/tls/certs/snooze.crt'}, clear=True)
    def test_nested_config(self, tmp_path):
        settings = CoreConfig(tmp_path)
        assert settings.ssl.certfile == Path('/etc/pki/tls/certs/snooze.crt')

    @patch.dict(os.environ, {'DATABASE_URL': 'mongodb://host01,host02,host03/snooze'}, clear=True)
    def test_mongo_environ(self, tmp_path):
        config = CoreConfig(tmp_path)
        assert config.database.type == 'mongo'

    @patch.dict(os.environ, {'SNOOZE_SERVER_CORE_AUDIT_EXCLUDED_PATHS': '/api1,/api2'}, clear=True)
    def test_array(self, tmp_path):
        config = CoreConfig(tmp_path)
        assert config.audit_excluded_paths == ['/api1', '/api2']

    @patch.dict(os.environ, {'SNOOZE_SERVER_CORE_DATABASE_TYPE': 'mongo', 'SNOOZE_SERVER_CORE_DATABASE_HOST': 'host01,host02,host03'})
    def test_nested_union(self, tmp_path):
        settings = CoreConfig(tmp_path)
        assert isinstance(settings.database, MongodbConfig)
