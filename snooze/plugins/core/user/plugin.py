#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

#!/usr/bin/python3.6

from snooze.plugins.core import Plugin
from datetime import datetime

import logging
from logging import getLogger
log = getLogger('snooze.user')

class User(Plugin):
    def manage_db(self, user_payload):
        now = datetime.now().astimezone().strftime("%Y-%m-%dT%H:%M:%S%z")
        method = user_payload['method']
        name = user_payload['name']
        if name == 'root' and method == 'root':
            log.warning("Root user detected! Will not create an account in the database")
            return (None, None)
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
        user['last_login'] = now
        new_groups = user_payload.get('groups') or []
        if old_groups != new_groups:
            log.debug("Will replace groups {} with {}".format(old_groups, new_groups))
            user['groups'] = new_groups
        if len(new_groups) > 0:
            query = ['IN', new_groups, 'groups']
            role_search = self.db.search('role', query)
            if role_search['count'] > 0:
                old_static_roles = user.get('static_roles') or []
                static_roles = list(map(lambda x: x['name'], role_search['data']))
                if old_static_roles != static_roles:
                    log.debug("Will replace static roles {} with {}".format(old_static_roles, static_roles))
                    user['static_roles'] = static_roles
                    user_roles = user.get('roles') or []
                    if user_roles:
                        log.debug("Will cleanup regular roles")
                        user['roles'] = [x for x in user_roles if x not in static_roles]
        primary = self.metadata.get('primary') or None
        display_name = user.pop('display_name', '')
        email = user.pop('email', '')
        self.db.write('user', user, primary)
        profile_search = self.db.search('profile.general', user_query)
        if profile_search['count'] > 0:
            log.debug("User {} profile already exists, skipping".format(name))
            pref_search = self.db.search('profile.preferences', user_query)
            if pref_search['count'] > 0:
                return (profile_search['data'][0], pref_search['data'][0])
            else:
                return (profile_search['data'][0], None)
        else:
            log.debug("Creating user {} profile: Display Name ({}), Email ({})".format(name, display_name, email))
            user_profile = {'name': name, 'method': method, 'display_name': display_name, 'email': email}
            self.db.write('profile.general', user_profile)
        return (None, None)
