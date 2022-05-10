#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''Module for handling the falcon WSGI'''

import os
import importlib.util
from abc import abstractmethod
from importlib import import_module
from logging import getLogger
from os.path import join as joindir
from pathlib import Path
from typing import Optional, List

import bson.json_util
import falcon
from pydantic import BaseModel, ValidationError
from falcon_auth import FalconAuthMiddleware, JWTAuthBackend

from snooze.api.routes import *
from snooze.health import HealthRoute
from snooze.utils.config import WebConfig

SNOOZE_GLOBAL_RUNDIR = '/var/run/snooze'
uid = os.getuid()
SNOOZE_LOCAL_RUNDIR = f"/var/run/user/{uid}"

class LoggerMiddleware(object):
    '''Middleware for logging'''

    def __init__(self, excluded_paths: List[str] = tuple()):
        self.logger = getLogger('snooze.audit')
        self.excluded_paths = excluded_paths

    def process_response(self, req, resp, *_args):
        '''Method for handling requests as a middleware'''
        source = req.access_route[0]
        method = req.method
        path = req.relative_uri
        status = resp.status[:3]
        message = f"{source} {method} {path} {status}"
        if not any(path.startswith(excluded) for excluded in self.excluded_paths):
            self.logger.debug(message)

class CORS:
    '''A falcon middleware to handle CORS when the snooze-server and
    snooze-web components are on different hosts.
    '''
    def __init__(self):
        pass

    def process_response(self, req, resp, _resource, req_succeeded):
        resp.set_header('Access-Control-Allow-Origin', '*')
        if (
            req_succeeded
            and req.method == 'OPTIONS'
            and req.get_header('Access-Control-Request-Method')
        ):
            allow = resp.get_header('Allow')
            resp.delete_header('Allow')

            allow_headers = req.get_header('Access-Control-Request-Headers', default='*')
            resp.set_headers(
                (
                    ('Access-Control-Allow-Methods', allow),
                    ('Access-Control-Allow-Headers', allow_headers),
                    ('Access-Control-Max-Age', '86400'),  # 24 hours
                )
            )

class Api:
    def __init__(self, core: 'Core'):
        self.core = core
        self.cluster = core.threads['cluster']

        # JWT setup
        self.secret = '' if self.core.config.core.no_login else self.core.secrets['jwt_private_key']
        def auth(payload):
            log.debug("Payload received: %s", payload.get('user', {}).get('name', payload))
            return payload
        self.jwt_auth = JWTAuthBackend(auth, self.secret)

        # Handler
        middlewares = [
            CORS(),
            LoggerMiddleware(self.core.config.core.audit_excluded_paths),
            FalconAuthMiddleware(self.jwt_auth),
        ]
        self.handler = falcon.API(middleware=middlewares)
        self.handler.req_options.auto_parse_qs_csv = False

        json_handler = falcon.media.JSONHandler(
            dumps=bson.json_util.dumps,
            loads=bson.json_util.loads,
        )
        self.handler.req_options.media_handlers.update({'application/json': json_handler})
        self.handler.resp_options.media_handlers.update({'application/json': json_handler})
        self.handler.add_error_handler(Exception, self.custom_handle_uncaught_exception)

        self.auth_routes = {}
        # Alert route
        self.add_route('/alert', AlertRoute(self))
        # List route
        self.add_route('/login', LoginRoute(self))
        # Reload route
        self.add_route('/reload', ReloadRoute(self))
        # Cluster route
        self.add_route('/cluster', ClusterRoute(self))
        # Health route
        self.add_route('/health', HealthRoute(self))
        # Permissions route
        self.add_route('/permissions', PermissionsRoute(self))
        # Basic auth setup
        self.auth_routes['local'] = LocalAuthRoute(self)
        self.add_route('/login/local', self.auth_routes['local'])
        # Anonymous auth
        if self.core.config.general.anonymous_enabled:
            self.auth_routes['anonymous'] = AnonymousAuthRoute(self)
            self.add_route('/login/anonymous', self.auth_routes['anonymous'])
        # Ldap auth
        self.auth_routes['ldap'] = LdapAuthRoute(self)
        self.add_route('/login/ldap', self.auth_routes['ldap'])
        # Optional metrics
        if self.core.stats.enabled:
            self.add_route('/metrics', MetricsRoute(self), '')

        web = self.core.config.core.web
        if web.enabled:
            self.add_route('/', RedirectRoute(), '')
            self.add_route('/web', RedirectRoute(), '')
            self.handler.add_sink(StaticRoute(web.path, '/web').on_get, '/web')

    def custom_handle_uncaught_exception(self, e, req, resp, params):
        '''Custom handler for logging uncaught exceptions in falcon inside python logger.
        Make use of an internal method of falcon to do so.
        '''
        log.exception(e)
        self.handler._compose_error_response(req, resp, HTTPInternalServerError())

    def add_route(self, route, action, prefix='/api'):
        '''Map a falcon route class to a given path'''
        self.handler.add_route(prefix + route, action)

    def get_root_token(self):
        '''Return a root token for the root user. Used only when requesting it from the internal unix socket'''
        return self.jwt_auth.get_auth_token({'name': 'root', 'method': 'root', 'permissions': ['rw_all']})

    def reload(self, config_name: str):
        reloaded_auth = []
        reloaded_conf = []

        try:
            config = self.core.config[config_name]
        except KeyError:
            return {'status': falcon.HTTP_404, 'text': f"Config '{config_name}' doesn't exist"}
        try:
            config.reload()
            for auth_backend in config._auth_routes:
                if self.auth_routes.get(auth_backend):
                    log.debug("Reloading %s auth backend", auth_backend)
                    self.auth_routes[auth_backend].reload()
                    reloaded_auth.append(auth_backend)
                else:
                    log.debug("Authentication backend '%s' not found", auth_backend)
            if len(reloaded_auth) > 0 or len(reloaded_conf) > 0:
                return {'status': falcon.HTTP_200, 'text': f"Reloaded auth '{reloaded_auth}' and conf {reloaded_conf}"}
            else:
                return {'status': falcon.HTTP_404, 'text': 'Error while reloading'}
        except Exception as err:
            log.exception(err)
            return {'status': falcon.HTTP_503}

    def write_and_reload(self, name: str, conf: dict, reload_conf, sync=False):
        '''Override the config files and reload. This is mainly used when changing the configuration
        from the web interface.
        '''
        result_dict = {}
        log.debug("Will write to %s config %s and reload %s", name, conf, reload_conf)
        if name and conf:
            try:
                config = self.core.config[name]
                config.update(conf)
            except (KeyError, AttributeError):
                return {'status': falcon.HTTP_404, 'text': f"Config '{name}' doesn't exist"}
            except Exception as err:
                return {'status': falcon.HTTP_503, 'text': str(err)}
            result_dict = {'status': falcon.HTTP_200, 'text': f"Reloaded config file {config._path}"}
        if reload_conf:
            auth_backends = reload_conf.get('auth_backends', [])
            if auth_backends:
                result_dict = self.reload(name)
            plugins = reload_conf.get('plugins', [])
            if plugins:
                result_dict = self.reload_plugins(plugins)
        if sync and self.cluster:
            self.cluster.write_and_reload(name, conf, reload_conf)
        return result_dict

    def reload_plugins(self, plugins):
        '''Reload plugins'''
        plugins_error = []
        plugins_success = []
        log.debug("Reloading plugins %s", plugins)
        for plugin_name in plugins:
            plugin = self.core.get_core_plugin(plugin_name)
            if plugin:
                plugin.reload_data()
                plugins_success.append(plugin)
            else:
                plugins_error.append(plugin)
        if plugins_error:
            return {'status': falcon.HTTP_404, 'text': f"The following plugins could not be found: {plugins_error}"}
        else:
            return {'status': falcon.HTTP_200, 'text': "Reloaded plugins: {plugin_success}"}

    def load_plugin_routes(self):
        log.debug('Loading plugin routes for API')
        for plugin in self.core.plugins:
            log.debug('Loading routes for %s at %s/falcon/route.py', plugin.name, plugin.rootdir)
            spec = importlib.util.spec_from_file_location(
                f"snooze.plugins.core.{plugin.name}.falcon.route",
                joindir(plugin.rootdir, 'falcon', 'route.py')
            )
            plugin_module = importlib.util.module_from_spec(spec)
            try:
                spec.loader.exec_module(plugin_module)
                log.debug("Found custom routes for `%s`", plugin.name)
            except FileNotFoundError:
                # Loading default
                log.debug("Loading default route for `%s`", plugin.name)
                plugin_module = import_module(f"snooze.plugins.core.basic.falcon.route")
            except Exception as err:
                log.exception(err)
                log.debug("Skip loading plugin `%s` routes", plugin.name)
                continue
            primary = plugin.metadata.get('primary') or None
            duplicate_policy = plugin.metadata.get('duplicate_policy') or 'update'
            authorization_policy = plugin.metadata.get('authorization_policy')
            check_permissions = plugin.metadata.get('check_permissions', False)
            check_constant = plugin.metadata.get('check_constant')
            injectpayload = plugin.metadata.get('inject_payload', False)
            prefix = plugin.metadata.get('prefix', '/api')
            for path, route in plugin.metadata.get('routes', {}).items():
                route_class_name = route['class']
                log.debug("For %s loading route class `%s`", path, route_class_name)
                route_class = getattr(plugin_module, route_class_name)
                route_duplicate_policy = route.get('duplicate_policy', duplicate_policy)
                route_authorization_policy = route.get('authorization_policy', authorization_policy)
                route_check_permissions = route.get('check_permissions', check_permissions)
                route_check_constant = route.get('check_constant', check_constant)
                route_injectpayload = route.get('inject_payload', injectpayload)
                route_prefix = route.get('prefix', prefix)
                log.debug("Route `%s` attributes: Primary (%s). Duplicate Policy (%s), Authorization Policy (%s), Check Permissions (%s), Check Constant (%s), Inject Payload (%s)", route_class_name, primary, route_duplicate_policy, route_authorization_policy, route_check_permissions, route_check_constant, route_injectpayload)
                self.add_route(path, route_class(self, plugin, primary, route_duplicate_policy, route_authorization_policy, route_check_permissions, route_check_constant, route_injectpayload), route_prefix)
