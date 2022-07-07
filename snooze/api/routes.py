#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''Module for handling the falcon WSGI'''

import mimetypes
import functools
from logging import getLogger
from hashlib import sha256
from hashlib import md5
from base64 import b64decode
from abc import abstractmethod
from typing import List, Optional, Union, ClassVar, Dict
from wsgiref.simple_server import WSGIServer
from socketserver import ThreadingMixIn
from pathlib import Path

from dataclasses import asdict

import falcon
import yaml
from falcon import Request, Response
from ldap3 import Server, Connection, ALL, SUBTREE
from ldap3.core.exceptions import LDAPOperationResult, LDAPExceptionError

from snooze import __file__ as rootdir
from snooze.utils.functions import ensure_kv, unique, authorize, extract_basic_auth
from snooze.utils.typing import DuplicatePolicy, AuthorizationPolicy, RouteArgs, AuthPayload
from snooze.db.database import Pagination

log = getLogger('snooze.api')

MAX_AGE = 24 * 3600

ConditionOrUid = Optional[Union[str, list]]

class ThreadingWSGIServer(ThreadingMixIn, WSGIServer):
    '''Daemonized threaded WSGI server'''
    daemon_threads = True

class BasicRoute:
    def __init__(self,
        api: 'Api',
        plugin: Optional[str] = None,
        route_args: RouteArgs = RouteArgs(),
    ):
        self.api = api
        self.core = api.core
        self.plugin = plugin
        self.options = route_args

    def search(self, collection: str, cond_or_uid: ConditionOrUid = None, **pagination: Pagination):
        '''Wrapping the search of an object by condition or uid. Also handling options for pagination'''
        if cond_or_uid is None:
            cond_or_uid = []
        if isinstance(cond_or_uid, list):
            return self.core.db.search(collection, cond_or_uid, **pagination)
        elif isinstance(cond_or_uid, str):
            return self.core.db.search(collection, ['=', 'uid', cond_or_uid], **pagination)
        else:
            return None

    def delete(self, collection: str, cond_or_uid: ConditionOrUid = None):
        '''Wrapping the delete of an object by condition or uid'''
        if cond_or_uid is None:
            cond_or_uid = []
        if isinstance(cond_or_uid, list):
            return self.core.db.delete(collection, cond_or_uid)
        elif isinstance(cond_or_uid, str):
            return self.core.db.delete(collection, ['=', 'uid', cond_or_uid])
        else:
            return None

    def insert(self, collection: str, record: Union[List[dict], dict]):
        '''Wrapping the insertion of a new object'''
        return self.core.db.write(collection, record,
            self.options.primary, self.options.duplicate_policy, constant=self.options.check_constant)

    def update(self, collection: str, record: Union[List[dict], dict]):
        '''Wrapping the update of an existing object'''
        return self.core.db.write(collection, record,
            self.options.primary, constant=self.options.check_constant)

    def get_roles(self, username: str, method: str) -> List[str]:
        '''Get the authorization roles for a user/auth method pair'''
        if username and method:
            log.debug("Getting roles for user %s (%s)", username, method)
            user = self.core.db.get_one('user', dict(name=username, method=method))
            if user:
                log.debug("User found in database: %s", user)
                roles = unique(user.get('roles', []) + user.get('static_roles', []))
                log.debug("User roles: %s", roles)
                return roles
            else:
                return []
        else:
            return []

    def get_permissions(self, roles: List[str]) -> List[str]:
        '''Return the permissions for a given list of roles'''
        if isinstance(roles, list) and len(roles) > 0:
            log.debug("Getting permissions for roles %s", roles)
            query = ['=', 'name', roles[0]]
            for role in roles[1:]:
                query = ['OR', ['=', 'name', role], query]
            role_search = self.core.db.search('role', query)
            permissions = []
            if role_search['count'] > 0:
                for role in role_search['data']:
                    permissions += role['permissions']
                permissions = unique(permissions)
                log.debug("List of permissions: %s", permissions)
                return permissions
            else:
                return []
        else:
            return []


class FalconRoute(BasicRoute):
    '''Basic falcon route'''
    def inject_payload_media(self, req, resp):
        auth: AuthPayload = req.context.auth
        log.debug("Injecting payload %s to %s", auth, req.media)
        if isinstance(req.media, list):
            for media in req.media:
                media['name'] = auth.username
                media['method'] = auth.method
        else:
            req.media['name'] = auth.username
            req.media['method'] = auth.method

    def inject_payload_search(self, req, s):
        auth: AuthPayload = req.context.auth
        to_inject = ['AND', ['=', 'name', auth.username], ['=', 'method', auth.method]]
        if s:
            return ['AND', s, to_inject]
        else:
            return to_inject

    def update_password(self, media):
        password = media.pop('password', None)
        name = media.get('name')
        method = media.get('method')
        if not password or not name or method != 'local':
            log.debug("Skipping updating password")
            return
        log.debug("Updating password for %s user %s", method, name)
        user_password = {}
        user_password['name'] = name
        user_password['method'] = method
        user_password['password'] = sha256(password.encode('utf-8')).hexdigest()
        self.core.db.write('user.password', user_password, 'name,method')

def merge_batch_results(rec_list):
    '''Merge the results (added/rejected/...) in the case of a batch'''
    return {'data': functools.reduce(lambda a, b: {k: a.get('data', {}).get(k, []) + b.get('data', {}).get(k, []) for k in list(dict.fromkeys(list(a.get('data', {}).keys()) + list(b.get('data', {}).keys())))}, rec_list)}

class WebhookRoute(FalconRoute):
    authentication = False

    @abstractmethod
    def parse_webhook(self, req, media):
        pass

    def on_post(self, req, resp):
        log.debug("Received webhook log %s", req.media)
        media = req.media.copy()
        rec_list = [{'data': {}}]
        if not isinstance(media, list):
            media = [media]
        for req_media in media:
            try:
                alerts = self.parse_webhook(req, req_media)
                if alerts:
                    if not isinstance(alerts, list):
                        alerts = [alerts]
                    for alert in alerts:
                        for key, val in req.params.items():
                            alert = ensure_kv(alert, val, *key.split('.'))
                        rec = self.core.process_record(alert)
                        rec_list.append(rec)
                else:
                    raise
            except Exception as e:
                log.exception(e)
                rec_list.append({'data': {'rejected': [req_media]}})
                continue
        resp.content_type = falcon.MEDIA_JSON
        resp.status = falcon.HTTP_200
        resp.media = merge_batch_results(rec_list)

class PermissionsRoute(BasicRoute):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.name = 'role'

    @authorize
    def on_get(self, req, resp):
        log.debug("Listing permissions")
        try:
            permissions = ['rw_all', 'ro_all']
            for plugin in self.api.core.plugins:
                permissions.append('rw_' + plugin.name)
                permissions.append('ro_' + plugin.name)
                for additional_permission in plugin.meta.permissions:
                    permissions.append(additional_permission)
            log.debug("List of permissions: {}".format(permissions))
            resp.content_type = falcon.MEDIA_JSON
            resp.status = falcon.HTTP_200
            resp.media = {
                'data': permissions,
            }
        except Exception as e:
            log.exception(e)
            resp.status = falcon.HTTP_503

class AlertRoute(BasicRoute):
    authentication = False

    def on_post(self, req, resp):
        log.debug("Received log %s", req.media)
        media = req.media.copy()
        if not isinstance(media, list):
            media = [media]
        data = {}
        for req_media in media:
            try:
                result = self.core.process_record(req_media)
                for action, records in result.items():
                    data.setdefault(action, [])
                    data[action] += records
            except Exception as err:
                log.exception(err)
                data.setdefault('rejected', [])
                data['rejected'].append(req_media)
                continue
        resp.content_type = falcon.MEDIA_JSON
        resp.status = falcon.HTTP_200
        resp.media = {'data': data}

class MetricsRoute(BasicRoute):
    '''A falcon route to serve prometheus metrics'''
    authentication = False

    def on_get(self, req, resp):
        try:
            resp.content_type = falcon.MEDIA_TEXT
            data = self.api.core.stats.get_metrics()
            resp.body = str(data.decode('utf-8'))
            resp.status = falcon.HTTP_200
        except Exception as err:
            log.exception(err)
            resp.status = falcon.HTTP_503

class LoginRoute(BasicRoute):
    '''A falcon route for users to login'''
    authentication = False

    def on_get(self, req, resp):
        log.debug("Listing authentication backends")
        try:
            backends = [
                {'name':self.api.auth_routes[backend].name, 'endpoint': backend}
                for backend in self.api.auth_routes.keys()
                if self.api.auth_routes[backend].enabled
            ]
            resp.content_type = falcon.MEDIA_JSON
            resp.status = falcon.HTTP_200
            default_auth_backend = self.core.config.general.default_auth_backend
            default_backends = [x for x in backends if x['endpoint'] == default_auth_backend]
            if len(default_backends) > 0:
                backends.remove(default_backends[0])
                backends.insert(0, default_backends[0])
            data = {'data': {'backends': backends}}
            if self.core.config.core.no_login:
                data['token'] = self.api.get_root_token()
            resp.media = data
        except Exception as err:
            log.exception(err)
            resp.status = falcon.HTTP_503

class ReloadPluginRoute(BasicRoute):
    '''A route to trigger the reload of a given plugin'''

    def on_post(self, req, resp, plugin_name: str):
        '''Trigger the reload of a plugin'''
        propagate = (req.params.get('propagate') is not None) # Key existence
        plugin = self.core.get_core_plugin(plugin_name)
        if plugin is None:
            raise falcon.HTTPNotFound(f"Plugin '{plugin_name}' not loaded in core")
        plugin.reload_data()
        if propagate:
            self.core.sync_reload_plugin(plugin_name)
            resp.status = falcon.HTTP_ACCEPTED
        else:
            resp.status = falcon.HTTP_OK

class ClusterRoute(BasicRoute):
    '''A route to fetch the status of the cluster member'''
    authentication = False

    def on_get(self, req, resp):
        '''Return the status of every cluster member'''
        cluster = self.core.threads['cluster']
        one = (req.params.get('one') is not None)
        if one:
            members = [cluster.status()]
        else:
            members = cluster.members_status()
        resp.content_type = falcon.MEDIA_JSON
        resp.status = falcon.HTTP_OK
        resp.media = {
            'data': [m.dict() for m in members],
        }

class SchemaRoute(BasicRoute):
    '''A route to return the form schema of each endpoint'''
    authentication = False

    schemas: ClassVar[Dict[str, dict]] = {}
    checksums: ClassVar[Dict[str, str]] = {}

    def __init__(self, *args, **kwargs):
        BasicRoute.__init__(self, *args, **kwargs)
        # Loading the web form schema at startup to avoid
        # runtime errors.
        self.schemas = {}
        self.checksums = {}
        basedir = Path(rootdir).parent / 'defaults/web'
        etcdir = Path('/etc/snooze/web')
        for path in basedir.glob('*.yaml'):
            custom_override = etcdir / f"{path.stem}.yaml"
            if custom_override.is_file():
                path = custom_override
            text = path.read_text(encoding='utf-8')
            self.checksums[path.stem] = md5(text.encode('utf8')).hexdigest()
            self.schemas[path.stem] = yaml.safe_load(text)

    def on_get(self, req: Request, resp: Response, endpoint: str):
        '''Return the form schema of a given endpoint'''
        try:
            endpoint_checksum = self.checksums[endpoint]
            if endpoint_checksum == req.params.get('checksum'):
                resp.status = falcon.HTTP_NOT_MODIFIED
            else:
                resp.media = self.schemas[endpoint]
                resp.set_header('CHECKSUM', endpoint_checksum)
                resp.status = falcon.HTTP_OK
        except KeyError as err:
            raise falcon.HTTPNotFound(f"No web config found for endpoint '{endpoint}'") from err

class AuthRoute(BasicRoute):
    authentication = False

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.auth_header_prefix = 'Basic'
        self.userplugin = self.api.core.get_core_plugin('user')
        self.enabled = True

    def parse_auth_token_from_request(self, auth_header):
        """
        Parses and returns Auth token from the request header. Raises
        `falcon.HTTPUnauthoried exception` with proper error message
        """
        if not auth_header:
            raise falcon.HTTPUnauthorized(
                description='Missing Authorization Header')

        parts = auth_header.split()

        if parts[0].lower() != self.auth_header_prefix.lower():
            raise falcon.HTTPUnauthorized(
                description=f"Invalid Authorization Header: Must start with {self.auth_header_prefix}")

        elif len(parts) == 1:
            raise falcon.HTTPUnauthorized(
                description='Invalid Authorization Header: Token Missing')
        elif len(parts) > 2:
            raise falcon.HTTPUnauthorized(
                description='Invalid Authorization Header: Contains extra content')

        return parts[1]

    def inject_permissions(self, auth: AuthPayload):
        '''Populate the roles and permissions in a given AuthPayload'''
        auth.roles = self.get_roles(auth.username, auth.method)
        auth.permissions = self.get_permissions(auth.roles)

    def on_post(self, req, resp):
        if self.enabled:
            auth = self.authenticate(req, resp)
            preferences = None
            if self.userplugin:
                _, preferences = self.userplugin.manage_db(auth)
            self.inject_permissions(auth)
            log.debug("Context user: %s", auth)
            token = self.core.token_engine.sign(auth)
            log.debug("Generated token: %s", token)
            resp.content_type = falcon.MEDIA_JSON
            resp.status = falcon.HTTP_200
            resp.media = {
                'token': token,
            }
            if preferences:
                resp.media['default_page'] = preferences.get('default_page')
        else:
            resp.content_type = falcon.MEDIA_JSON
            resp.status = falcon.HTTP_409
            resp.media = {
                'response': 'Backend disabled',
            }

    @abstractmethod
    def authenticate(self, req, resp) -> AuthPayload:
        '''Abstract method called to authenticate the user.
        Is expected to return an AuthPayload, and to raise
        falcon.HTTPUnauthorized when unauthorized.
        '''

    @abstractmethod
    def reload(self):
        '''Abstract method to reload the configuration. Usually make
        use of snooze.utils.config to do so.'''

class AnonymousAuthRoute(AuthRoute):
    '''An authentication route for anonymous users'''
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.name = 'Anonymous'
        self.reload()

    def reload(self):
        self.core.config.general.refresh()
        self.enabled = self.core.config.general.anonymous_enabled
        log.debug("Authentication backend 'anonymous' status: %s", self.enabled)

    def authenticate(self, req, resp):
        log.debug('Anonymous login')
        return AuthPayload(username='anonymous', method='anonymous')

class LocalAuthRoute(AuthRoute):
    '''An authentication route for local users'''
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.name = 'Local'
        self.reload()

    def reload(self):
        self.core.config.general.refresh()
        self.enabled = self.core.config.general.local_users_enabled
        log.debug("Authentication backend 'local' status: %s", self.enabled)

    def authenticate(self, req, resp):
        username, password = extract_basic_auth(req)
        password_hash = sha256(password.encode('utf-8')).hexdigest()
        log.debug("Attempting login for %s, with password hash %s", username, password_hash)
        user = self.core.db.get_one('user', dict(name=username, method='local'))
        try:
            if user:
                passwd = self.core.db.get_one('user.password', dict(name=username, method='local'))
                if not passwd:
                    raise falcon.HTTPUnauthorized(description='Password not found')
                try:
                    db_password = passwd['password']
                except KeyError:
                    raise falcon.HTTPUnauthorized(description='Invalid password entry in database')

                if db_password == password_hash:
                    log.debug('Password was correct for user %s', username)
                    return AuthPayload(username=username, method='local')
                else:
                    log.debug('Password was incorrect for user %s', username)
                    raise falcon.HTTPUnauthorized(
                		description='Invalid Username/Password')
            else:
                log.debug('User %s does not exist', username)
                raise falcon.HTTPUnauthorized(
                	description='User does not exist')
        except Exception as e:
            log.exception('Exception while trying to compare passwords')
            raise falcon.HTTPUnauthorized(
           		description='Exception while trying to compare passwords')

class LdapAuthRoute(AuthRoute):
    '''An authentication route for LDAP users'''
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.name = 'Ldap'
        self.enabled = False
        self.config = self.core.config.ldap_auth
        self.reload()

    def reload(self):
        self.config.refresh()
        self.enabled = self.config.enabled
        if self.enabled:
            try:
                if '://' in self.config.host:
                    uri = self.config.host
                else:
                    if self.config.port == 636:
                        uri = f"ldaps://{self.config.host}"
                    else:
                        uri = f"ldap://{self.config.host}"
                self.server = Server(uri, port=self.config.port, get_info=ALL, connect_timeout=10)
                bind_con = Connection(
                    self.server,
                    user=self.config.bind_dn,
                    password=self.config.bind_password,
                    raise_exceptions=True
                )
                if not bind_con.bind():
                    log.error("Cannot BIND to LDAP server: %s:%s", uri, self.config.port)
                    self.enabled = False
            except Exception as err:
                log.exception(err)
                self.enabled = False
        log.debug("Authentication backend 'ldap'. Enabled: %s", self.enabled)

    def _search_user(self, username):
        try:
            bind_con = Connection(
                self.server,
                user=self.config.bind_dn,
                password=self.config.bind_password,
                raise_exceptions=True
            )
            bind_con.bind()
            user_filter = self.config.user_filter.replace('%s', username)
            bind_con.search(
                search_base = self.config.base_dn,
                search_filter = user_filter,
                attributes = [
                    self.config.display_name_attribute,
                    self.config.email_attribute,
                    self.config.member_attribute,
                ],
                search_scope = SUBTREE
            )
            response = bind_con.response
            if (
                bind_con.result['result'] == 0
                and len(response) > 0
                and 'dn' in response[0].keys()
            ):
                user_dn = response[0]['dn']
                attributes = response[0]['attributes']
                groups = [
                    group for group in attributes[self.config.member_attribute]
                    for dn in self.config.group_dn.split(':')
                    if group.endswith(dn)
                ]
                return {'name': username, 'dn': user_dn, 'groups': groups}
            else:
                # Could not find user in search
                raise falcon.HTTPUnauthorized(description=f"Error in search: Could not find user {username} in LDAP search")
        except LDAPOperationResult as err:
            raise falcon.HTTPUnauthorized(description=f"Error during search: {err}")
        except LDAPExceptionError as err:
            raise falcon.HTTPUnauthorized(description=f"Error during search: {err}")

    def _bind_user(self, user_dn, password):
        try:
            user_con = Connection(
                self.server,
                user=user_dn,
                password=password,
                raise_exceptions=True
            )
            user_con.bind()
            return user_con
        except LDAPOperationResult as err:
            raise falcon.HTTPUnauthorized(description=f"Error during bind: {err}")
        except LDAPExceptionError as err:
            raise falcon.HTTPUnauthorized(description=f"Error during bind: {err}")
        finally:
            user_con.unbind()

    def authenticate(self, req, resp):
        username, password = extract_basic_auth(req)
        user = self._search_user(username)
        user_con = self._bind_user(user['dn'], password)
        if user_con.result['result'] == 0:
            groups = [group.split(',')[0].split('=', 1)[-1] for group in user['groups']]
            return AuthPayload(username=user['name'], method='ldap', groups=groups)
        else:
            raise falcon.HTTPUnauthorized(description=f"Wrong LDAP username or password for '{user['dn']}'")

class RedirectRoute:
    '''A falcon route for managing the default redirection'''
    authentication = False

    def on_get(self, req, resp):
        raise falcon.HTTPMovedPermanently('/web/')

class StaticRoute:
    '''Handler route for static files (for the web server)'''
    root: Path
    prefix: str
    indexes: List[str]

    def __init__(self, root, prefix='', indexes=('index.html',)):
        self.prefix = prefix
        self.indexes = indexes
        self.root = root

    def on_get(self, req: Request, resp: Response):
        '''Serve a file like an HTTP file server'''
        file = req.path[len(self.prefix):]
        if len(file) > 0 and file.startswith('/'):
            file = file[1:]

        path: Path = self.root / file
        path.resolve()

        # Prevent top level access
        if self.root not in [path, *path.parents]:
            raise falcon.HTTPForbidden(f"Request path {req.path} is trying to escape root ({self.root})")

        # Search for index if directory
        if path.is_dir():
            index = self.search_index(path)
            if not index:
                raise falcon.HTTPNotFound()
            filepath = index
        elif path.is_file():
            filepath = path
        else:
            raise falcon.HTTPNotFound()

        # Type and encoding
        content_type, _encoding = mimetypes.guess_type(filepath)
        if content_type is not None:
            resp.content_type = content_type

        try:
            resp.data = filepath.read_bytes()
            resp.cache_control = [f"max-age={MAX_AGE}"]
        except OSError:
            raise falcon.HTTPForbidden()

    def search_index(self, path: Path) -> Optional[Path]:
        '''Return the index file when requesting a directory'''
        for index in self.indexes:
            index_file = path / index
            if index_file.is_file():
                return index_file
        return None
