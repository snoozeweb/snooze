#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#


import importlib.util
from importlib import import_module
from logging import getLogger
from os.path import join as joindir
from abc import abstractmethod

from snooze.utils import Cluster

log = getLogger('snooze.api')

class BasicRoute():
    def __init__(self, api, plugin=None, primary=None, duplicate_policy='update', authorization_policy=None, check_permissions=False, check_constant=None, inject_payload=False):
        self.api = api
        self.core = api.core
        self.plugin = plugin
        self.primary = primary
        self.duplicate_policy = duplicate_policy
        self.authorization_policy = authorization_policy
        self.check_permissions = check_permissions
        self.check_constant = check_constant
        self.inject_payload = inject_payload

    def search(self, collection, cond_or_uid=None, nb_per_page=0, page_number=1, order_by='', asc=True):
        '''Wrapping the search of an object by condition or uid. Also handling options for pagination'''
        if cond_or_uid is None:
            cond_or_uid = []
        if isinstance(cond_or_uid, list):
            return self.core.db.search(collection, cond_or_uid, nb_per_page, page_number, order_by, asc)
        elif isinstance(cond_or_uid, str):
            return self.core.db.search(collection, ['=', 'uid', cond_or_uid], nb_per_page, page_number, order_by, asc)
        else:
            return None

    def delete(self, collection, cond_or_uid=None):
        '''Wrapping the delete of an object by condition or uid'''
        if cond_or_uid is None:
            cond_or_uid = []
        if isinstance(cond_or_uid, list):
            return self.core.db.delete(collection, cond_or_uid)
        elif isinstance(cond_or_uid, str):
            return self.core.db.delete(collection, ['=', 'uid', cond_or_uid])
        else:
            return None

    def insert(self, collection, record):
        '''Wrapping the insertion of a new object'''
        return self.core.db.write(collection, record, self.primary, self.duplicate_policy, constant=self.check_constant)

    def update(self, collection, record):
        '''Wrapping the update of an existing object'''
        return self.core.db.write(collection, record, self.primary, constant = self.check_constant)

    def get_roles(self, username, method):
        '''Get the authorization roles for a user/auth method pair'''
        if username and method:
            log.debug("Getting roles for user %s (%s)", username, method)
            user_search = self.core.db.search('user', ['AND', ['=', 'name', username], ['=', 'method', method]])
            if user_search['count'] > 0:
                user = user_search['data'][0]
                log.debug("User found in database: %s", user)
                roles = list(set((user.get('roles') or []) + (user.get('static_roles') or [])))
                log.debug("User roles: %s", roles)
                return roles
            else:
                return []
        else:
            return []

    def get_permissions(self, roles):
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
                permissions = list(set(permissions))
                log.debug("List of permissions: %s", permissions)
                return permissions
            else:
                return []
        else:
            return []

class Api:
    def __init__(self, core):
        self.conf = core.conf
        self.plugins = core.plugins
        self.core = core
        self.api_type = self.conf.get('api', {}).get('type', 'falcon')
        cls = import_module(f"snooze.api.{self.api_type}")
        self.__class__ = type('Api', (cls.BackendApi, Api), {})
        self.init_api(core)
        self.load_plugin_routes()
        self.cluster = Cluster(self)
        self.core.cluster = self.cluster

    def load_plugin_routes(self):
        log.debug('Loading plugin routes for API')
        for plugin in self.plugins:
            log.debug('Loading routes for %s at %s/%s/route.py', plugin.name, plugin.rootdir, self.api_type)
            spec = importlib.util.spec_from_file_location(
                f"snooze.plugins.core.{plugin.name}.{self.api_type}.route",
                joindir(plugin.rootdir, self.api_type, 'route.py')
            )
            plugin_module = importlib.util.module_from_spec(spec)
            try:
                spec.loader.exec_module(plugin_module)
                log.debug("Found custom routes for `%s`", plugin.name)
            except FileNotFoundError:
                # Loading default
                log.debug("Loading default route for `%s`", plugin.name)
                plugin_module = import_module(f"snooze.plugins.core.basic.{self.api_type}.route")
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

    @abstractmethod
    def init_api(self, *args, **kwargs): pass

    @abstractmethod
    def add_route(self, route, action, prefix): pass
