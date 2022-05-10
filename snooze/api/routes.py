#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''Module for handling the falcon WSGI'''

import os.path
import mimetypes
import functools
from logging import getLogger
from hashlib import sha256
from base64 import b64decode
from abc import abstractmethod
from typing import List, Optional, Union
from wsgiref.simple_server import WSGIServer
from socketserver import ThreadingMixIn

from dataclasses import asdict

import falcon
from ldap3 import Server, Connection, ALL, SUBTREE
from ldap3.core.exceptions import LDAPOperationResult, LDAPExceptionError

from snooze.utils.functions import ensure_kv, unique, authorize
from snooze.utils.typing import DuplicatePolicy, AuthorizationPolicy
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
        primary: Optional[str] = None,
        duplicate_policy: DuplicatePolicy = 'update',
        authorization_policy: Optional[AuthorizationPolicy] = None,
        check_permissions: bool = False,
        check_constant: Optional[str] = None,
        inject_payload: bool = False
    ):
        self.api = api
        self.core = api.core
        self.plugin = plugin
        self.primary = primary
        self.duplicate_policy = duplicate_policy
        self.authorization_policy = authorization_policy
        self.check_permissions = check_permissions
        self.check_constant = check_constant
        self.inject_payload = inject_payload

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

    def insert(self, collection: str, record: dict):
        '''Wrapping the insertion of a new object'''
        return self.core.db.write(collection, record, self.primary, self.duplicate_policy, constant=self.check_constant)

    def update(self, collection: str, record: dict):
        '''Wrapping the update of an existing object'''
        return self.core.db.write(collection, record, self.primary, constant = self.check_constant)

    def get_roles(self, username: str, method: str) -> List[str]:
        '''Get the authorization roles for a user/auth method pair'''
        if username and method:
            log.debug("Getting roles for user %s (%s)", username, method)
            user_search = self.core.db.search('user', ['AND', ['=', 'name', username], ['=', 'method', method]])
            if user_search['count'] > 0:
                user = user_search['data'][0]
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
        user_payload = req.context['user']['user']
        log.debug("Injecting payload %s to %s", user_payload, req.media)
        if isinstance(req.media, list):
            for media in req.media:
                media['name'] = user_payload['name']
                media['method'] = user_payload['method']
        else:
            req.media['name'] = user_payload['name']
            req.media['method'] = user_payload['method']

    def inject_payload_search(self, req, s):
        user_payload = req.context['user']['user']
        to_inject = ['AND', ['=', 'name', user_payload['name']], ['=', 'method', user_payload['method']]]
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
    auth = {
        'auth_disabled': True
    }

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
                for additional_permission in plugin.permissions:
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
    auth = {
        'auth_disabled': True
    }

    def on_post(self, req, resp):
        log.debug("Received log %s", req.media)
        media = req.media.copy()
        rec_list = [{'data': {}}]
        if not isinstance(media, list):
            media = [media]
        for req_media in media:
            try:
                rec = self.core.process_record(req_media)
                rec_list.append(rec)
            except Exception as e:
                log.exception(e)
                rec_list.append({'data': {'rejected': [req_media]}})
                continue
        resp.content_type = falcon.MEDIA_JSON
        resp.status = falcon.HTTP_200
        resp.media = merge_batch_results(rec_list)

class MetricsRoute(BasicRoute):
    '''A falcon route to serve prometheus metrics'''
    auth = {
        'auth_disabled': True
    }

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
    auth = {
        'auth_disabled': True
    }

    def on_get(self, req, resp):
        log.debug("Listing authentication backends")
        if self.core.config.core.no_login:
            resp.content_type = falcon.MEDIA_JSON
            resp.status = falcon.HTTP_200
            resp.media = {
                'token': self.api.get_root_token(),
            }
            return
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
            resp.media = {
                'data': {'backends': backends},
            }
        except Exception as err:
            log.exception(err)
            resp.status = falcon.HTTP_503

class ReloadRoute(BasicRoute):
    '''A falcon route to reload one's token'''
    auth = {
        'auth_disabled': True
    }

    def on_post(self, req, resp):
        media = req.media.copy()
        if media.get('reload_token', '-') == self.api.core.secrets.get('reload_token', '+'):
            filename = media.get('filename')
            conf = media.get('conf')
            _reload = media.get('reload')
            sync = media.get('sync', False)
            log.debug("Reloading conf (%s, %s), backend %s, sync %s", filename, conf, _reload, sync)
            results = self.api.write_and_reload(filename, conf, _reload, sync)
            resp.content_type = falcon.MEDIA_TEXT
            resp.status = results.get('status', falcon.HTTP_503)
            resp.text = results.get('text', '')
        else:
            resp.status = falcon.HTTP_401
            resp.text = 'Invalid secret reload token'

class ClusterRoute(BasicRoute):
    auth = {
        'auth_disabled': True
    }

    def on_get(self, req, resp):
        log.debug("Listing cluster members")
        cluster = self.core.threads['cluster']
        if req.params.get('self', False):
            members = [cluster.status()]
        else:
            members = cluster.members_status()
        resp.content_type = falcon.MEDIA_JSON
        resp.status = falcon.HTTP_200
        resp.media = {
            'data': [asdict(m) for m in members],
        }

class AuthRoute(BasicRoute):
    auth = {
        'auth_disabled': True
    }

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

    def _extract_credentials(self, req):
        auth = req.get_header('Authorization')
        token = self.parse_auth_token_from_request(auth_header=auth)
        try:
            token = b64decode(token).decode('utf-8')

        except Exception:
            raise falcon.HTTPUnauthorized(
                description='Invalid Authorization Header: Unable to decode credentials')

        try:
            username, password = token.split(':', 1)
        except ValueError:
            raise falcon.HTTPUnauthorized(
                description='Invalid Authorization: Unable to decode credentials')

        return username, password

    def parse_user(self, user):
        return user

    def inject_permissions(self, user):
        roles = self.get_roles(user['name'], user['method'])
        permissions = self.get_permissions(roles)
        user['roles'] = roles
        user['permissions'] = permissions

    def on_post(self, req, resp):
        if self.enabled:
            self.authenticate(req, resp)
            user = self.parse_user(req.context['user'])
            preferences = None
            if self.userplugin:
                _, preferences = self.userplugin.manage_db(user)
            self.inject_permissions(user)
            log.debug("Context user: %s", user)
            token = self.api.jwt_auth.get_auth_token(user)
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
    def authenticate(self, req, resp):
        '''Abstract method called to authenticate the user.
        Is expected to set req.context['user'], and to raise
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
        req.context['user'] = 'anonymous'

    def parse_user(self, user):
        return {'name': 'anonymous', 'method': 'local'}

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
        username, password = self._extract_credentials(req)
        password_hash = sha256(password.encode('utf-8')).hexdigest()
        log.debug("Attempting login for %s, with password hash %s", username, password_hash)
        user_search = self.core.db.search('user', ['AND', ['=', 'name', username], ['=', 'method', 'local']])
        try:
            if user_search['count'] > 0:
                query = ['AND', ['=', 'name', username], ['=', 'method', 'local']]
                db_password_search = self.core.db.search('user.password', query)
                try:
                    db_password = db_password_search['data'][0]['password']
                except Exception as _err:
                    raise falcon.HTTPUnauthorized(
                		description='Password not found')
                if db_password == password_hash:
                    log.debug('Password was correct for user %s', username)
                    req.context['user'] = username
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

    def parse_user(self, user):
        return {'name': user, 'method': 'local'}

class LdapAuthRoute(AuthRoute):
    '''An authentication route for LDAP users'''
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.name = 'Ldap'
        self.enabled = False
        self.config = self.core.config.ldap
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
        username, password = self._extract_credentials(req)
        user = self._search_user(username)
        user_con = self._bind_user(user['dn'], password)
        if user_con.result['result'] == 0:
            req.context['user'] = user
        else:
            raise falcon.HTTPUnauthorized(description="")

    def parse_user(self, user):
        groups = list(map(lambda x: x.split(',')[0].split('=')[1], user['groups']))
        return {'name': user['name'], 'groups': groups, 'method': 'ldap'}

class RedirectRoute:
    '''A falcon route for managing the default redirection'''
    auth = {
        'auth_disabled': True
    }
    def on_get(self, req, resp):
        raise falcon.HTTPMovedPermanently('/web/')

class StaticRoute:
    '''Handler route for static files (for the web server)'''
    def __init__(self, root, prefix='', indexes=('index.html',)):
        self.prefix = prefix
        self.indexes = indexes
        self.root = root

    def on_get(self, req, res):
        file = req.path[len(self.prefix):]

        if len(file) > 0 and file.startswith('/'):
            file = file[1:]

        path = os.path.join(self.root, file)
        path = os.path.abspath(path)

        # Prevent top level access
        if not path.startswith(self.root):
            res.stats = falcon.HTTP_403
            return

        # Search for index if directory
        if os.path.isdir(path):
            path = self.search_index(path)
            if not path:
                res.stats = falcon.HTTP_404
                return

        # Type and encoding
        content_type, _encoding = mimetypes.guess_type(path)
        if content_type is not None:
            res.content_type = content_type

        try:
            with open(path, 'rb') as static_file:
                res.cache_control = [f"max-age={MAX_AGE}"]
                res.text = static_file.read()
        except FileNotFoundError as err:
            res.status = falcon.HTTP_404

    def search_index(self, path):
        '''Return the index file when requesting a directory'''
        for index in self.indexes:
            index_file = os.path.join(path, index)
            if os.path.isfile(index_file):
                return index_file
        return None
