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

from snooze.api.routes import *
from snooze.health import HealthRoute
from snooze.token import TokenAuthMiddleware
from snooze.utils.config import WebConfig
from snooze.utils.functions import log_error_handler, log_warning_handler, log_uncaught_handler
from snooze.utils.typing import HTTPUserErrors

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

class Api:
    def __init__(self, core: 'Core'):
        self.core = core
        self.cluster = core.threads['cluster']

        # Handler
        middlewares = [
            falcon.CORSMiddleware(allow_origins='*', allow_credentials='*'),
            LoggerMiddleware(self.core.config.core.audit_excluded_paths),
        ]
        middlewares.append(TokenAuthMiddleware(self.core.token_engine))
        self.handler = falcon.API(middleware=middlewares)
        self.handler.req_options.auto_parse_qs_csv = False

        json_handler = falcon.media.JSONHandler(
            dumps=bson.json_util.dumps,
            loads=bson.json_util.loads,
        )
        self.handler.req_options.media_handlers.update({'application/json': json_handler})
        self.handler.resp_options.media_handlers.update({'application/json': json_handler})
        self.handler.add_error_handler(HTTPUserErrors, log_warning_handler)
        self.handler.add_error_handler(falcon.HTTPError, log_error_handler)
        self.handler.add_error_handler(Exception, log_uncaught_handler)

        self.auth_routes = {}
        # Alert route
        self.add_route('/alert', AlertRoute(self))
        # List route
        self.add_route('/login', LoginRoute(self))
        # Reload route
        self.add_route('/reload/{plugin_name}', ReloadPluginRoute(self))
        # Cluster route
        self.add_route('/cluster', ClusterRoute(self))
        # Health route
        self.add_route('/health', HealthRoute(self))
        # Schema route
        self.add_route('/schema/{endpoint}', SchemaRoute(self))
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

    def add_route(self, route, action, prefix='/api'):
        '''Map a falcon route class to a given path'''
        self.handler.add_route(prefix + route, action)

    def get_root_token(self) -> str:
        '''Return a root token for the root user. Used only when requesting it from the internal unix socket'''
        auth = AuthPayload(username='root', method='root', permissions=['rw_all'])
        return self.core.token_engine.sign(auth)

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
            for path, route_args in plugin.meta.routes.items():
                log.debug("For %s loading route: %s", path, route_args.dict())
                if route_args.class_name is not None:
                    instance = getattr(plugin_module, route_args.class_name)(self, plugin, route_args)
                    log.debug("Adding route %s: %s", path, instance)
                    self.add_route(path, instance)
