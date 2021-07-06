#!/usr/bin/python3.6

from snooze.plugins.core import Plugin

import logging
from logging import getLogger
log = getLogger('snooze.user')

class User(Plugin):
    def manage_db(self, user_payload):
        write_db = False
        method = user_payload['method']
        name = user_payload['name']
        if name == 'root' and method == 'root':
            log.warning("Root user detected! Will not create an account in the database")
            return
        user_query = ['AND', ['=', 'name', name], ['=', 'method', method]]
        user_search = self.db.search('user', user_query)
        log.debug("Searching in users for user {} with method {}".format(name, method))
        if user_search['count'] > 0:
            user = user_search['data'][0]
            log.debug("User found: {}".format(user))
            old_groups = user.get('groups') or []
        else:
            log.debug("User not found, adding them to the database")
            user = user_payload
            old_groups = []
            write_db = True
        new_groups = user_payload.get('groups') or []
        if old_groups != new_groups:
            log.debug("Will replace groups {} with {}".format(old_groups, new_groups))
            user['groups'] = new_groups
            write_db = True
        if len(new_groups) > 0:
            query = ['IN', new_groups, 'groups']
            role_search = self.db.search('role', query)
            if role_search['count'] > 0:
                old_static_roles = user.get('static_roles') or []
                static_roles = list(map(lambda x: x['name'], role_search['data']))
                if old_static_roles != static_roles:
                    log.debug("Will replace static roles {} with {}".format(old_static_roles, static_roles))
                    user['static_roles'] = static_roles
                    write_db = True
                    user_roles = user.get('roles') or []
                    if user_roles:
                        log.debug("Will cleanup regular roles")
                        user['roles'] = [x for x in user_roles if x not in static_roles]
        if write_db:
            primary = self.metadata.get('primary') or None
            display_name = user.pop('display_name', '')
            email = user.pop('email', '')
            self.db.write('user', user, primary)
            profile_search = self.db.search('profile.general', user_query)
            if profile_search['count'] > 0:
                log.debug("User {} profile already exists, skipping".format(name))
            else:
                log.debug("Creating user {} profile: Display Name ({}), Email ({})".format(name, display_name, email))
                user_profile = {'name': name, 'method': method, 'display_name': display_name, 'email': email}
                self.db.write('profile.general', user_profile)
