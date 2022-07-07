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
from typing import Optional, List, Any, Dict, Literal, ClassVar, Union, Callable, Type

import yaml
from filelock import FileLock
from pydantic import BaseModel, Field, PrivateAttr, validator, root_validator, ValidationError, Extra
from pydantic.config import BaseConfig
from pydantic.fields import ModelField
from pydantic.utils import deep_update

from snooze import __file__ as SNOOZE_PATH
from snooze.utils.typing import *

log = getLogger('snooze.utils.config')

SNOOZE_CONFIG = Path(os.environ.get('SNOOZE_SERVER_CONFIG', '/etc/snooze/server'))
SNOOZE_PLUGIN_PATH = Path(SNOOZE_PATH).parent / 'plugins/core'

class ReadOnlyConfig(BaseModel):
    '''Similar to pydantic.BaseSettings, load a BaseModel from different sources
    (yaml config files and environment variables).
    Follow the same structure as pydantic.BaseSettings.
    Can be reloaded with the reload method.
    '''
    _path: Path = PrivateAttr()

    def __init__(self, _basedir: Path = SNOOZE_CONFIG, **data):
        object.__setattr__(self, '_path', _basedir / f"{self.__config__.section}.yaml")
        BaseModel.__init__(self, **self._load_data(**data))

    class Config(BaseConfig):
        # Custom variable used for sources
        section: str
        # We're using assignments for the refresh
        validate_assignment = True

        @classmethod
        def prepare_field(cls, field: ModelField) -> None:
            '''Preparing auto-environment variables'''
            section = cls.section
            env = field.field_info.extra.get('env', [])
            if isinstance(env, str):
                env = [env]
            else:
                env = list(env)
            env_names = [f"SNOOZE_SERVER_{section}_{field.name}"] + env
            field.field_info.extra['env_names'] = env_names

    __config__: ClassVar[Type[Config]]

    def __setattr__(self, _key, _value):
        '''Overridding the __setattr__ to virtually make this class
        immutable'''
        raise TypeError(f"{self.__class__.__name__} is immutable")

    def _load_data(self, **data) -> dict:
        '''Load data from different sources'''
        log.debug("Loading config %s", self._path)
        env_settings = EnvSettingsSource()
        yaml_settings = SnoozeSettingsSource(self._path)
        # deep_update take the left argument, and deep merge the right
        # arguments one by one
        return deep_update(yaml_settings(self), env_settings(self), data)

    def refresh(self):
        '''Reload the data from source (config and env variables)'''
        data = self._load_data()
        for key, value in data.items():
            BaseModel.__setattr__(self, key, value)

class SnoozeSettingsSource:
    '''A config source of information, pydantic style'''
    def __init__(self, path: Path):
        self.path = path

    def __call__(self, settings: ReadOnlyConfig) -> Dict[str, Any]:
        try:
            text = self.path.read_text(encoding='utf-8')
            data = yaml.safe_load(text) or {}
            return data
        except OSError:
            return {}

class EnvSettingsSource:
    '''A config loader for environment variables'''
    def __call__(self, settings: ReadOnlyConfig) -> Dict[str, Any]:
        section = settings.__config__.section
        envs = {
            k.split('_', 3)[-1].lower(): v
            for k, v in os.environ.items()
            if k.startswith(f"SNOOZE_SERVER_{section}_")
        }
        for field in settings.__fields__.values():
            env_names = field.field_info.extra.get('env_names', [])
            value = next((os.environ[env] for env in env_names if env in os.environ), None)
            if value is not None:
                envs[field.alias] = value
        return envs

@contextmanager
def lock_and_flush(path: Path, flush: Callable):
    '''Lock the file, yield, and execute a flush callable at the end'''
    if not path.is_file():
        path.touch(mode=0o600)
    lockfile = path.parent / f"{path.name}.lock"
    lock = FileLock(lockfile, timeout=1)
    lock.acquire()
    try:
        yield
        flush()
    except Exception as err:
        raise RuntimeError(f"Error while updating config at {path}: {err}") from err
    finally:
        lock.release()
        lockfile.unlink(missing_ok=True)

class WritableConfig(ReadOnlyConfig):
    '''A class representing a writable config file at a given path.
    Can be explored, and updated with a lock file.'''

    class Config:
        # Custom config
        auth_routes: List[str] = []
        # Setting values should trigger validation
        validate_assignment = True
        allow_mutation = True

    def __init__(self, _basedir: Path = SNOOZE_CONFIG, **data):
        ReadOnlyConfig.__init__(self, _basedir, **data)

    def auth_routes(self) -> List[str]:
        '''Return the list of auth routes to reload'''
        return self.__config__.auth_routes

    def set(self, key: str, value: Any):
        '''Rewrite a config key with a given value'''
        # Ignore updates of falsy values for excluded keys.
        # Handle the password update case, since we're not returning
        # the value for every excluded field.
        excluded = getattr(self, '__exclude_fields__') or {}
        if key in excluded.keys() and not value:
            return
        with lock_and_flush(self._path, self.flush):
            BaseModel.__setattr__(self, key, value)

    def update(self, values: dict):
        '''Update the config with a dictionary'''
        log.debug("Updating config %s", self._path)
        excluded = getattr(self, '__exclude_fields__') or {}
        for key in excluded.keys():
            if not values.get(key):
                values.pop(key, None)
        with lock_and_flush(self._path, self.flush):
            clone = self.copy(update=values, deep=True)
            self.__dict__.update(clone.__dict__)

    def __setattr__(self, key: str, value: Any):
        self.set(key, value)

    def __setitem__(self, key: str, value: Any):
        self.set(key, value)

    def flush(self):
        '''Flush the live config to the config file'''
        # dict(model) is more appropriate than model.dict()
        # in this case since it's including excluded fields
        data = dict(self)
        # Filtering the private fields used by ReadOnlyConfig and WritableConfig
        # (starting with '_')
        data = {k: v for k, v in data.items() if not k.startswith('_')}
        text = yaml.safe_dump(data)
        self._path.write_text(text, encoding='utf-8')

class MetadataConfig(BaseModel):
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
    force_order: Optional[str] = None
    tree: Optional[bool] = False

    def __init__(self, plugin_name: str, moduledir: Optional[Path] = None):
        object.__setattr__(self, 'name', plugin_name)
        object.__setattr__(self, '_moduledir', moduledir)
        data = self._load_data()
        try:
            BaseModel.__init__(self, **data)
        except ValidationError as err:
            raise ValidationError("Cannot load metadata for plugin {self.name}: {err}") from err

    def _load_data(self) -> Dict[str, Any]:
        core_path = SNOOZE_PLUGIN_PATH / self.name / 'metadata.yaml'
        alt_path = self._moduledir / 'metadata.yaml' if self._moduledir else None

        if core_path.is_file():
            path = core_path
        elif alt_path and alt_path.is_file():
            path = alt_path
        else:
            log.debug("Could not find metadata.yaml for plugin '%s'", self.name)
            return {}
        try:
            text = path.read_text(encoding='utf-8')
            data = yaml.safe_load(text)
            return data
        except OSError:
            return {}

    def reload(self):
        '''Reload the data from metadata.yaml'''
        data = self._load_data()
        for key, value in data.items():
            BaseModel.__setattr__(self, key, value)

class LdapConfig(WritableConfig):
    '''Configuration for LDAP authentication. Can be edited live in the web interface.
    Usually located at `/etc/snooze/server/ldap_auth.yaml`.'''
    class Config:
        title = 'LDAP configuration'
        section = 'ldap_auth'
        auth_routes = ['ldap']

    @root_validator
    def validate_enabled(cls, values):
        if values.get('enabled'):
            for field in ['base_dn', 'user_filter', 'bind_dn', 'bind_password', 'host']:
                if values.get(field) is None:
                    raise ValueError(f"field {field} should not be null when the config is enabled")
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

DatabaseConfig = Union[MongodbConfig, FileConfig]

class CoreConfig(ReadOnlyConfig):
    '''Core configuration. Not editable live. Require a restart of the server.
    Usually located at `/etc/snooze/server/core.yaml`'''
    class Config:
        title = 'Core configuration'
        section = 'core'

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
        env='DATABASE_URL',
        default_factory=FileConfig,
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

    @validator('database', pre=True)
    def parse_url(cls, database):
        '''Parse the database if given in a URL form'''
        if isinstance(database, str):
            scheme = urlparse(database).scheme
            if scheme == 'mongodb':
                return MongodbConfig(host=database)
            else:
                raise ValueError(f"Unsupported scheme {scheme} for given database URL")
        return database

class GeneralConfig(WritableConfig):
    '''General configuration of snooze. Can be edited live in the web interface.
    Usually located at `/etc/snooze/server/general.yaml`.'''
    class Config:
        title = 'General configuration'
        section = 'general'
        auth_routes = ['local']

    default_auth_backend: Literal['local', 'ldap', 'anonymous'] = Field(
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
    class Config:
        title='Notification configuration'
        section = 'notifications'
        json_encoders = {
            # timedelta should be serialized into seconds (int)
            timedelta: lambda dt: int(dt.total_seconds()),
        }

    def dict(self, **kwargs):
        data = BaseModel.dict(self, **kwargs)
        for key, value in data.items():
            if isinstance(value, timedelta):
                data[key] = value.total_seconds()
        return data

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

class HousekeeperConfig(WritableConfig):
    '''Config for the housekeeper thread. Can be edited live in the web interface.
    Usually located at `/etc/snooze/server/housekeeper.yaml`.'''
    class Config:
        title = 'Housekeeper configuration'
        section = 'housekeeping'
        json_encoders = {
            # timedelta should be serialized into seconds (int)
            timedelta: lambda dt: int(dt.total_seconds()),
        }

    def dict(self, **kwargs):
        data = BaseModel.dict(self, **kwargs)
        for key, value in data.items():
            if isinstance(value, timedelta):
                data[key] = value.total_seconds()
        return data

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
    cleanup_orphans: timedelta = Field(
        title='Cleanup orphans',
        description='Time (in seconds) between each run of orphans cleaning',
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
        for _, handler in logging_dict.get('loggers', {}).items():
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
    housekeeping: HousekeeperConfig
    notifications: NotificationConfig
    ldap_auth: LdapConfig

    def __init__(self, basedir: Path = SNOOZE_CONFIG):
        configs = {
            'basedir': basedir,
            'core': CoreConfig(basedir),
            'general': GeneralConfig(basedir),
            'notifications': NotificationConfig(basedir),
            'housekeeping': HousekeeperConfig(basedir),
        }
        try:
            configs['ldap_auth'] = LdapConfig(basedir)
        except (FileNotFoundError, ValidationError):
            configs['ldap_auth'] = LdapConfig(basedir, dict(enabled=False))
        BaseModel.__init__(self, **configs)
