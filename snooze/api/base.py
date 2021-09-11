#!/usr/bin/python
import os
import json

from importlib import import_module
from wsgiref.simple_server import make_server, WSGIServer
from socketserver import ThreadingMixIn
from snooze.utils import Cluster

from logging import getLogger
log = getLogger('snooze.api')

class ThreadingWSGIServer(ThreadingMixIn, WSGIServer):
    daemon_threads = True

class BasicRoute():
    def __init__(self, api, plugin = None, primary = None, duplicate_policy = 'update', authorization_policy = None, check_permissions = False, check_constant = None, inject_payload = False):
        self.api = api
        self.core = api.core
        self.plugin = plugin
        self.primary = primary
        self.duplicate_policy = duplicate_policy
        self.authorization_policy = authorization_policy
        self.check_permissions = check_permissions
        self.check_constant = check_constant
        self.inject_payload = inject_payload

    def search(self, collection, cond_or_uid=[], nb_per_page=0, page_number=1, order_by='', asc=True):
        if type(cond_or_uid) is list:
            return self.core.db.search(collection, cond_or_uid, nb_per_page, page_number, order_by, asc)
        elif type(cond_or_uid) is str:
            return self.core.db.search(collection, ['=', 'uid', cond_or_uid], nb_per_page, page_number, order_by, asc)
        else:
            return None

    def delete(self, collection, cond_or_uid=[]):
        if type(cond_or_uid) is list:
            return self.core.db.delete(collection, cond_or_uid)
        elif type(cond_or_uid) is str:
            return self.core.db.delete(collection, ['=', 'uid', cond_or_uid])
        else:
            return None

    def insert(self, collection, record):
        return self.core.db.write(collection, record, self.primary, self.duplicate_policy, constant = self.check_constant)

    def update(self, collection, record):
        return self.core.db.write(collection, record, self.primary, constant = self.check_constant)

    def get_roles(self, name, method):
        if name and method:
            log.debug("Getting roles for user {} ({})".format(name, method))
            user_search = self.core.db.search('user', ['AND', ['=', 'name', name], ['=', 'method', method]])
            if user_search['count'] > 0:
                user = user_search['data'][0]
                log.debug("User found in database: {}".format(user))
                roles = list(set((user.get('roles') or []) + (user.get('static_roles') or [])))
                log.debug("User roles: {}".format(roles))
                return roles
            else:
                return []
        else:
            return []

    def get_permissions(self, roles):
        if isinstance(roles, list) and len(roles) > 0:
            log.debug("Getting permissions for roles {}".format(roles))
            query = ['=', 'name', roles[0]]
            for role in roles[1:]:
                query = ['OR', ['=', 'name', role], query]
            role_search = self.core.db.search('role', query)
            permissions = []
            if role_search['count'] > 0:
                for role in role_search['data']:
                    permissions += role['permissions']
                permissions = list(set(permissions))
                log.debug("List of permissions: {}".format(permissions))
                return permissions
            else:
                return []
        else:
            return []

class Api():
    def __init__(self, core, use_socket=True):
        self.conf = core.conf
        self.plugins = core.plugins
        self.core = core
        self.api_type = self.conf.get('api', {}).get('type', 'falcon')
        cls = import_module("snooze.api.{}".format(self.api_type))
        self.__class__ = type('Api', (cls.BackendApi, Api), {})
        self.init_api(core, use_socket)
        self.load_plugin_routes()
        self.cluster = Cluster(self)
        self.core.cluster = self.cluster

    def load_plugin_routes(self):
        log.debug('Loading plugin routes for API')
        for plugin in self.plugins:
            log.debug('Loading routes for {}'.format(plugin.name))
            try:
                import_module("snooze.plugins.core.{}".format(plugin.name))
            except ModuleNotFoundError:
                log.debug("Module for plugin `{}` not found. Using Basic instead".format(plugin.name))
            try:
                plugin_module = import_module("snooze.plugins.core.{}.{}.route".format(plugin.name, self.api_type))
                log.debug("Found custom routes for {}".format(plugin.name))
            except ModuleNotFoundError:
                # Loading default
                log.debug("Loading default route for `{}`".format(plugin.name))
                plugin_module = import_module("snooze.plugins.core.basic.{}.route".format(self.api_type))
            primary = plugin.metadata.get('primary') or None
            duplicate_policy = plugin.metadata.get('duplicate_policy') or 'update'
            authorization_policy = plugin.metadata.get('authorization_policy')
            check_permissions = plugin.metadata.get('check_permissions', False)
            check_constant = plugin.metadata.get('check_constant')
            injectpayload = plugin.metadata.get('inject_payload', False)
            prefix = plugin.metadata.get('prefix', '/api')
            for path, route in plugin.metadata.get('routes', {}).items():
                route_class_name = route['class']
                log.debug("For {} loading route class `{}`".format(path, route_class_name))
                route_class = getattr(plugin_module, route_class_name)
                route_duplicate_policy = route.get('duplicate_policy', duplicate_policy)
                route_authorization_policy = route.get('authorization_policy', authorization_policy)
                route_check_permissions = route.get('check_permissions', check_permissions)
                route_check_constant = route.get('check_constant', check_constant)
                route_injectpayload = route.get('inject_payload', injectpayload)
                route_prefix = route.get('prefix', prefix)
                log.debug("Route `{}` attributes: Primary ({}). Duplicate Policy ({}), Authorization Policy ({}), Check Permissions ({}), Check Constant ({}), Inject Payload ({})".format(route_class_name, primary, route_duplicate_policy, route_authorization_policy, route_check_permissions, route_check_constant, route_injectpayload))
                self.add_route(path, route_class(self, plugin, primary, route_duplicate_policy, route_authorization_policy, route_check_permissions, route_check_constant, route_injectpayload), route_prefix)

    def init_api(self): pass

    def add_route(self, route, action, prefix): pass

    def serve(self):
        httpd = make_server('0.0.0.0', 9000, self.handler, ThreadingWSGIServer)
        httpd.serve_forever()
