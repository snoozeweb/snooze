#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''Module for managing loading and writing the configuration files'''

import os
import logging
from contextlib import contextmanager
from logging import getLogger
from pathlib import Path
from datetime import timedelta
from urllib.parse import urlparse
from typing import Optional, List, Any, Dict, Literal, ClassVar, Union

import yaml
from filelock import FileLock
from pydantic import BaseModel, Field, validator, root_validator, ValidationError, Extra

from snooze import __file__ as SNOOZE_PATH
from snooze.utils.typing import *

log = getLogger('snooze.utils.config')

SNOOZE_CONFIG = Path(os.environ.get('SNOOZE_SERVER_CONFIG', '/etc/snooze/server'))
SNOOZE_PLUGIN_PATH = Path(SNOOZE_PATH).parent / 'plugins/core'

class ReadOnlyConfig(BaseModel):
    '''A class representing a config file at a given path.
    Can only be read.'''
    _section: ClassVar[Optional[str]] = None
    _path: ClassVar[Optional[Path]] = None

    class Config:
        allow_mutation = False

    def __init__(self, basedir: Path = SNOOZE_CONFIG, data: Optional[dict] = None):
        #section = self._class_get('_section')
        if self._section:
            #self._class_set('_path', basedir / f"{section}.yaml")
            self.__class__._path = basedir / f"{self._section}.yaml"
        data = data or self._read() or {}
        BaseModel.__init__(self, **data)

    def _class_get(self, key: str):
        '''Get a class attribute'''
        return getattr(self.__class__, key)

    def _class_set(self, key: str, value: Any):
        '''Set a class attribute'''
        # Using this workaround to avoid pydantic and WritableConfig own __setattr__
        setattr(self.__class__, key, value)

    def _read(self) -> dict:
        '''Read the config file and return the raw dict'''
        if self._path:
            try:
                return yaml.safe_load(self._path.read_text(encoding='utf-8')) or {}
            except OSError:
                return {}
        else:
            return {}

    def refresh(self):
        '''Read the config file to load the config'''
        data = self._read()
        for key, value in data.items():
            setattr(self, key, value)

    def __getitem__(self, key: str):
        return getattr(self, key)

    def dig(self, *keys: List[str], default: Optional[Any] = None) -> Any:
        '''Get a nested key from the config'''
        try:
            cursor = getattr(self, keys[0])
            for key in keys[1:]:
                cursor = getattr(cursor, key)
            return cursor
        except AttributeError:
            return default

class WritableConfig(ReadOnlyConfig):
    '''A class representing a writable config file at a given path.
    Can be explored, and updated with a lock file.'''
    _filelock: ClassVar[FileLock] = None
    _auth_routes: ClassVar[List[str]] = Field(default_factory=list)

    class Config:
        # Setting values should trigger validation
        validate_assignment = True
        allow_mutation = True

    def __init__(self, basedir: Path = SNOOZE_CONFIG, data: Optional[dict] = None):
        if data is None:
            data = {}
        ReadOnlyConfig.__init__(self, basedir, data)
        self._path.touch(mode=0o600)
        self.__class__._filelock = FileLock(self._path, timeout=1)

    @contextmanager
    def _lock(self):
        self._class_get('_filelock').acquire()
        self.refresh()
        try:
            yield # Update the config
        finally:
            self._update()
            self._class_get('_filelock').release()

    def _update(self):
        '''Write a new config to the config file'''
        path = self._class_get('_path')
        if path:
            # dict(model) is more appropriate than model.dict()
            # in this case since it's including excluded fields
            data = yaml.safe_dump(dict(self))
            path.write_text(data, encoding='utf-8')

    def __setitem__(self, key: str, value: Any):
        self._set(key, value)

    def __setattr__(self, key: str, value: Any):
        self._set(key, value)

    def _set(self, key: str, value: Any):
        '''Rewrite a config key with a given value'''
        with self._lock():
            object.__setattr__(self, key, value)

    def update(self, values: dict):
        '''Update the config with a dictionary'''
        with self._lock():
            for key, value in values.items():
                object.__setattr__(self, key, value)

class MetadataConfig(ReadOnlyConfig):
    '''A class to fetch metadata configuration'''
    name: Optional[str] = None
    desc: Optional[str] = None
    class_name: Optional[str] = Field('Route', alias='class')
    auto_reload: bool = False
    default_sorting: Optional[str] = None
    default_ordering: bool = True
    audit: bool = True
    widgets: Dict[str, Widget] = Field(default_factory=dict)
    action_form: dict  = Field(default_factory=dict)
    provides: List[str] = Field(default_factory=list)
    routes: Dict[str, RouteArgs] = Field(default_factory=dict)
    route_defaults: RouteArgs = RouteArgs()
    icon: str = 'question-circle'
    options: dict = Field(default_factory=dict)
    search_fields: List[str] = Field(default_factory=list)

    def __init__(self, plugin_name: str):
        path = SNOOZE_PLUGIN_PATH / plugin_name / 'metadata.yaml'
        self._class_set('_path', path)
        data = self._read() or {}
        try:
            BaseModel.__init__(self, **data)
        except ValidationError as err:
            raise Exception(f"Cannot load metadata for plugin {plugin_name}") from err

class LdapConfig(WritableConfig, title='LDAP configuration'):
    '''Configuration for LDAP authentication. Can be edited live in the web interface.
    Usually located at `/etc/snooze/server/ldap_auth.yaml`.'''
    _section = 'ldap_auth'
    _auth_routes = ['ldap']

    @root_validator
    def validate_enabled(cls, values):
        if values.get('enabled'):
            for field in ['base_dn', 'user_filter', 'bind_dn', 'bind_password', 'host']:
                assert values.get(field)
        return values

    enabled: bool = Field(
        description='Enable or disable LDAP Authentication',
        default=False,
    )
    base_dn: Optional[str] = Field(
        title='Base DN',
        default=None,
        description='LDAP users location. Multiple DNs can be added if separated by semicolons',
    )
    user_filter: Optional[str] = Field(
        title='User base filter',
        default=None,
        description='LDAP search filter for the base DN',
        examples=['(objectClass=posixAccount)'],
    )
    bind_dn: Optional[str] = Field(
        title='Bind DN',
        default=None,
        description='Distinguished name to bind to the LDAP server',
        examples=['CN=john.doe,OU=users,DC=example,DC=com'],
    )
    bind_password: Optional[str] = Field(
        title='Bind DN password',
        description='Password for the Bind DN user',
        default=None,
        exclude=True,
    )
    host: Optional[str] = Field(
        description='LDAP host',
        default=None,
        examples=['ldaps://example.com'],
    )
    port: int = Field(
        default=636,
        description='LDAP server port',
    )
    group_dn: Optional[str] = Field(
        title='Group DN',
        default=None,
        description='Base DN used to filter out groups. Will default to the User base DN'
        ' Multiple DNs can be added if separated by semicolons',
    )
    email_attribute: str = Field(
        title='Email attribute',
        default='mail',
        description='User attribute that displays the user email adress',
    )
    display_name_attribute: str = Field(
        title='Display name attribute',
        default='cn',
        description='User attribute that displays the user real name',
    )
    member_attribute: str = Field(
        title='Member attribute',
        default='memberof',
        description='Member attribute that displays groups membership',
    )

class SslConfig(BaseModel):
    '''SSL configuration'''

    @root_validator
    def validate_enabled(cls, values):
        '''Throw an error if the setting is enabled but a mandatory option is
        not set'''
        if values.get('enabled'):
            for field in ['certfile', 'keyfile']:
                assert values.get(field)
        return values

    enabled: bool = Field(
        default=False,
        description='Enabling TLS termination',
    )
    certfile: Optional[Path] = Field(
        title='Certificate file',
        env='SNOOZE_CERT_FILE',
        default=None,
        description='Path to the x509 PEM style certificate to use for TLS termination',
        examples=['/etc/pki/tls/certs/snooze.crt', '/etc/ssl/certs/snooze.crt'],
    )
    keyfile: Optional[Path] = Field(
        title='Key file',
        default=None,
        env='SNOOZE_KEY_FILE',
        description='Path to the private key to use for TLS termination',
        examples=['/etc/pki/tls/private/snooze.key', '/etc/ssl/private/snooze.key'],
    )

class WebConfig(BaseModel):
    '''The subconfig for the web server (snooze-web)'''
    enabled: bool = Field(
        default=True,
        description='Enable the web interface',
    )
    path: Path = Field(
        default='/opt/snooze/web',
        description='Path to the web interface dist files',
    )

class BackupConfig(BaseModel):
    '''Configuration for the backup job'''

    enabled: bool = Field(
        default=True,
        description='Enable backups',
    )
    path: Path = Field(
        default=Path('/var/lib/snooze'),
        env='SNOOZE_BACKUP_PATH',
        description='Path to store database backups',
    )
    excludes: List[str] = Field(
        description='Collections to exclude from backups',
        default=['record', 'stats', 'comment', 'secrets'],
    )

class ClusterConfig(BaseModel):
    '''Configuration for the cluster'''

    enabled: bool = Field(
        default=False,
        description='Enable clustering. Required when running multiple backends',
    )
    members: List[HostPort] = Field(
        env='SNOOZE_CLUSTER',
        default_factory=lambda: [HostPort(host='localhost')],
        description='List of snooze servers in the cluster. If the environment variable is provided,'
        ' a special syntax is expected (`"<host>:<port>,<host>:<port>,..."`).',
        examples=[
            [
                {'host': 'host01', 'port': 5200},
                {'host': 'host02', 'port': 5200},
                {'host': 'host03', 'port': 5200},
            ],
            "host01:5200,host02:5200,host03:5200",
        ],

    )

    @validator('members')
    def parse_members_env(cls, value):
        '''In case the environment (a string) is passed, parse the environment string'''
        if isinstance(value, str):
            members = []
            for member in value.split(','):
                members.append(HostPort(member.split(':', 1)))
            return members
        return value

class MongodbConfig(BaseModel, extra=Extra.allow):
    '''Mongodb configuration passed to pymongo MongoClient'''
    type: Literal['mongo'] = 'mongo'
    host: Optional[Union[str, List[str]]] = Field(
        title='Host',
        default=None,
        env='DATABASE_URL',
        description='Hostname or IP address or Unix domain socket path of a single mongod or mongos instance'
        'to connect to',
    )
    port: Optional[int] = Field(
        title='Port',
        default=None,
        description='Port number on which to connect',
    )

class FileConfig(BaseModel, extra=Extra.allow):
    type: Literal['file'] = 'file'
    path: Path = Path(f"{os.getcwd()}/db.json")

def select_db() -> Union[MongodbConfig, FileConfig]:
    '''Return the correct database config type'''
    if 'DATABASE_URL' in os.environ:
        scheme = urlparse(os.environ['DATABASE_URL']).scheme
        if scheme == 'mongodb':
            return MongodbConfig()
    return FileConfig()

DatabaseConfig = Union[MongodbConfig, FileConfig]

class CoreConfig(ReadOnlyConfig, title='Core configuration'):
    '''Core configuration. Not editable live. Require a restart of the server.
    Usually located at `/etc/snooze/server/core.yaml`'''
    _section = 'core'

    listen_addr: str = Field(
        title='Listening address',
        default='0.0.0.0',
        description="IPv4 address on which Snooze process is listening to",
    )
    port: int = Field(
        default=5200,
        description='Port on which Snooze process is listening to',
    )
    debug: bool = Field(
        default=False,
        env='SNOOZE_DEBUG',
        description='Activate debug log output',
    )
    bootstrap_db: bool = Field(
        title='Bootstrap database',
        default=True,
        description='Populate the database with an initial configuration',
    )
    unix_socket: Optional[Path] = Field(
        title='Unix socket',
        default='/var/run/snooze/server.socket',
        description='Listen on this unix socket to issue root tokens',
    )
    no_login: bool = Field(
        title='No login',
        default=False,
        env='SNOOZE_NO_LOGIN',
        description='Disable Authentication (everyone has admin priviledges)',
    )
    audit_excluded_paths: List[str] = Field(
        title='Audit excluded paths',
        default=['/api/patlite', '/metrics', '/web'],
        description='A list of HTTP paths excluded from audit logs. Any path'
        'that starts with a path in this list will be excluded.',
    )
    process_plugins: List[str] = Field(
        title='Process plugins',
        default=['rule', 'aggregaterule', 'snooze', 'notification'],
        description='List of plugins that will be used for processing alerts.'
        ' Order matters.',
    )
    database: DatabaseConfig = Field(
        title='Database',
        default_factory=select_db,
    )
    init_sleep: int = Field(
        title='Init sleep',
        default=5,
        description='Time to sleep before retrying certain operations (bootstrap, clustering)',
    )
    create_root_user: bool = Field(
        title='Create root user',
        default=True,
        description='Create a *root* user with a default password *root*',
    )
    ssl: SslConfig = Field(
        title='SSL configuration',
        default_factory=SslConfig,
    )
    web: WebConfig = Field(
        title='Web server configuration',
        default_factory=WebConfig,
    )
    cluster: ClusterConfig = Field(
        title='Cluster configuration',
        default_factory=ClusterConfig,
    )
    backup: BackupConfig = Field(
        title='Backup configuration',
        default_factory=BackupConfig,
    )

class GeneralConfig(WritableConfig, title='General configuration'):
    '''General configuration of snooze. Can be edited live in the web interface.
    Usually located at `/etc/snooze/server/general.yaml`.'''
    _section = 'general'
    _auth_routes = ['local']

    default_auth_backend: Literal['local', 'ldap'] = Field(
        title='Default authentication backend',
        description='Backend that will be first in the list of displayed authentication backends',
        default='local',
    )
    local_users_enabled: bool = Field(
        title='Local users enabled',
        description='Enable the creation of local users in snooze. This can be disabled when another'
        ' reliable authentication backend is used, and the admin want to make auditing easier',
        default=True,
    )
    metrics_enabled: bool = Field(
        title='Metrics enabled',
        description='Enable Prometheus metrics',
        default=True,
    )
    anonymous_enabled: bool = Field(
        title='Anonymous enabled',
        description='Enable anonymous user login. When a user log in as anonymous, he will be given user permissions',
        default=False,
    )
    ok_severities: List[str] = Field(
        title='OK severities',
        description='List of severities that will automatically close the aggregate upon entering the system.'
        ' This is mainly for icinga/grafana that can close the alert when the status becomes green again',
        default=['ok', 'success'],
    )

    @validator('ok_severities', each_item=True)
    def normalize_severities(cls, value):
        '''Normalizing severities upon retrieval and insertion'''
        return value.casefold()

class NotificationConfig(WritableConfig):
    '''Configuration for default notification delays/retry. Can be edited live in the web interface.
    Usually located at `/etc/snooze/server/notifications.yaml`.'''
    _section = 'notifications'

    notification_freq: timedelta = Field(
        title='Frequency',
        description='Time (in seconds) to wait before sending the next notification',
        default=timedelta(minutes=1),
    )
    notification_retry: int = Field(
        title='Retry number',
        description='Number of times to retry sending a failed notification',
        default=3,
    )
    class Config:
        title = 'Notification configuration'
        json_encoders = {
            # timedelta should be serialized into seconds (int)
            timedelta: lambda dt: int(dt.total_seconds()),
        }

class HousekeeperConfig(WritableConfig):
    '''Config for the housekeeper thread. Can be edited live in the web interface.
    Usually located at `/etc/snooze/server/housekeeper.yaml`.'''
    _section = 'housekeeper'

    trigger_on_startup: bool = Field(
        title='Trigger on startup',
        default=True,
        description='Trigger all housekeeping job on startup',
    )
    record_ttl: timedelta = Field(
        title='Record Time-To-Live',
        description='Default TTL (in seconds) for alerts incoming',
        default=timedelta(days=2),
    )
    cleanup_alert: timedelta = Field(
        title='Cleanup alert',
        description='Time (in seconds) between each run of alert cleaning. Alerts that exceeded their TTL '
        ' will be deleted',
        default=timedelta(minutes=5),
    )
    cleanup_comment: timedelta = Field(
        title='Cleanup comment',
        description='Time (in seconds) between each run of comment cleaning. Comments which are not bound to'
        ' any alert will be deleted',
        default=timedelta(days=1),
    )
    cleanup_audit: timedelta = Field(
        title='Cleanup audit',
        description='Cleanup orphans audit logs that are older than the given duration (in seconds). Run daily',
        default=timedelta(days=28),
    )
    cleanup_snooze: timedelta = Field(
        title='Cleanup snooze',
        description="Cleanup snooze filters that have been expired for the given duration (in seconds). Run daily",
        default=timedelta(days=3),
    )
    cleanup_notification: timedelta = Field(
        title='Cleanup notifications',
        description='Cleanup notifications that have been expired for the given duration (in seconds). Run daily',
        default=timedelta(days=3),
    )

    class Config:
        title = 'Housekeeper configuration'
        json_encoders = {
            # timedelta should be serialized into seconds (int)
            timedelta: lambda dt: int(dt.total_seconds()),
        }

def setup_logging(basedir: Path = SNOOZE_CONFIG):
    '''Initialize the python logger'''
    try:
        logging_file = basedir / 'logging.yaml'
        logging_dict = yaml.safe_load(logging_file.read_text(encoding='utf-8'))
    except FileNotFoundError:
        logging_dict = {
            'version': 1,
            'disable_existing_loggers': False,
            'formatters': {
                'simple': {
                    'format': '%(asctime)s %(name)-20s %(levelname)-8s %(message)s',
                },
            },
            'handlers': {
                'console': {
                    'class': 'logging.StreamHandler',
                    'level': 'INFO',
                    'formatter': 'simple',
                    'stream': 'ext://sys.stdout',
                },
            },
            'loggers': {
                'snooze': {
                    'level': 'INFO',
                    'handlers': ['console'],
                    'propagate': False,
                },
            },
        }

    debug = CoreConfig(basedir).debug
    if debug:
        for _, handler in logging_dict.get('handlers', {}).items():
            handler['level'] = 'DEBUG'

    logging.config.dictConfig(logging_dict)
    log = getLogger('snooze')
    log.debug("Log system ON")
    return log

class Config(BaseModel):
    '''An object representing the complete snooze configuration'''
    basedir: Path

    core: CoreConfig
    general: GeneralConfig
    housekeeper: HousekeeperConfig
    notifications: NotificationConfig
    ldap: LdapConfig

    def __init__(self, basedir: Path = SNOOZE_CONFIG):
        configs = {
            'basedir': basedir,
            'core': CoreConfig(basedir),
            'general': GeneralConfig(basedir),
            'notifications': NotificationConfig(basedir),
            'housekeeper': HousekeeperConfig(basedir),
        }
        try:
            configs['ldap'] = LdapConfig(basedir)
        except (FileNotFoundError, ValidationError):
            configs['ldap'] = LdapConfig(basedir, dict(enabled=False))
        BaseModel.__init__(self, **configs)
