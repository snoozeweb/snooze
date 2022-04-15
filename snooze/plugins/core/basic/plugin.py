#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

import sys
import os
from logging import getLogger
from os.path import dirname, join as joindir
from abc import abstractmethod

import yaml

from snooze import __file__ as rootdir
from snooze.utils.typing import Record, Config

log = getLogger('snooze')

class Plugin:
    def __init__(self, core: 'Core'):
        self.core = core
        self.db = core.db
        self.name = self.__class__.__name__.lower()
        self.data = []
        self.rootdir = joindir(dirname(rootdir), 'plugins', 'core', self.name)
        metadata_path = joindir(self.rootdir, 'metadata.yaml')
        if not os.access(metadata_path, os.R_OK):
            self.rootdir = dirname(sys.modules[self.__module__].__file__)
            metadata_path = joindir(self.rootdir, 'metadata.yaml')
        self.metadata_file = {}
        try:
            log.debug("Attempting to read metadata at %s for %s module", metadata_path, self.name)
            with open(metadata_path, 'r', encoding='utf-8') as metadata_file:
                self.metadata_file = yaml.safe_load(metadata_file.read())
        except (OSError, yaml.YAMLError) as err:
            log.debug("Skipping. Cannot read metadata.yaml due to: %s", err)
        self.permissions = self.metadata_file.get('provides', [])
        default_routeclass = self.metadata_file.get('class', 'Route')
        default_authorization = self.metadata_file.get('authorization_policy')
        default_duplicate = self.metadata_file.get('duplicate_policy', 'update')
        default_checkpermissions = self.metadata_file.get('check_permissions', False)
        default_checkconstant = self.metadata_file.get('check_constant')
        default_injectpayload = self.metadata_file.get('inject_payload', False)
        default_prefix = self.metadata_file.get('prefix', '/api')
        if self.metadata_file.get('action_form'):
            self.metadata_file['action_name'] = self.name
        if default_routeclass:
            routes = {
                '/'+self.name: {
                    'class': default_routeclass,
                    'authorization_policy': default_authorization,
                    'duplicate_policy': default_duplicate,
                    'check_permissions': default_checkpermissions,
                    'check_constant': default_checkconstant,
                    'inject_payload': default_injectpayload,
                    'prefix': default_prefix
                },
                '/'+self.name+'/{search}': {
                    'class': default_routeclass,
                    'authorization_policy': default_authorization,
                    'duplicate_policy': default_duplicate,
                    'check_permissions': default_checkpermissions,
                    'check_constant': default_checkconstant,
                    'inject_payload': default_injectpayload,
                    'prefix': default_prefix
                },
                '/'+self.name+'/{search}/{perpage}/{pagenb}': {
                    'class': default_routeclass,
                    'authorization_policy': default_authorization,
                    'duplicate_policy': default_duplicate,
                    'check_permissions': default_checkpermissions,
                    'check_constant': default_checkconstant,
                    'inject_payload': default_injectpayload,
                    'prefix': default_prefix
                },
                '/'+self.name+'/{search}/{perpage}/{pagenb}/{orderby}/{asc}': {
                    'class': default_routeclass,
                    'authorization_policy': default_authorization,
                    'duplicate_policy': default_duplicate,
                    'check_permissions': default_checkpermissions,
                    'check_constant': default_checkconstant,
                    'inject_payload': default_injectpayload,
                    'prefix': default_prefix
                }
            }
            if 'routes' in self.metadata_file:
                routes.update(self.metadata_file['routes'])
            self.metadata = {
                'name': self.name,
                'auto_reload': self.metadata_file.get('auto_reload', False),
                'default_sorting': self.metadata_file.get('default_sorting', None),
                'default_ordering': self.metadata_file.get('default_ordering', True),
                'primary': self.metadata_file.get('primary', None),
                'widgets': self.metadata_file.get('widgets', {}),
                'action_form': self.metadata_file.get('action_form', {}),
                'routes': routes,
                'audit': self.metadata_file.get('audit', True),
            }
        else:
            self.metadata = self.metadata_file
        search_fields = self.metadata_file.get('search_fields', [])
        if search_fields:
            self.db.create_index(self.name, search_fields)

    def validate(self, obj: dict):
        '''Validate an object before writing it to the database.
        Should raise an exception if the object is invalid
        '''

    def post_init(self):
        '''Hook to execute something after the default init'''
        self.reload_data()

    def reload_data(self, sync: bool = False):
        '''Reload the data of a plugin from the database'''
        if self.metadata.get('auto_reload', False):
            log.debug("Reloading data for plugin %s", self.name)
            pagination = {}
            if 'default_sorting' in self.metadata:
                pagination['orderby'] = self.metadata['default_sorting']
            if 'asc' in self.metadata:
                pagination['asc'] = self.metadata['default_ordering']
            self.data = self.db.search(self.name, **pagination)['data']

    def sync_neighbors(self):
        '''Trigger the reload of the module to neighbors (async)'''
        self.core.threads['cluster'].reload_plugin(self.name)

    def process(self, record: Record) -> Record:
        '''Process a record if it's a process plugin'''
        return record

    def get_metadata(self) -> dict:
        '''Returned the metadata of the plugin (from the parsed yaml file)'''
        return self.metadata

    def pprint(self, options: dict = {}) -> str:
        return self.name

    def get_icon(self) -> str:
        '''Return the icon of the plugin, for web interface usage'''
        return self.metadata_file.get('icon', 'question-circle')

    def get_options(self, key: str = '') -> dict:
        options = self.metadata_file.get('options', {})
        if key:
            options = options.get(key, {})
        return options

    @abstractmethod
    def send(self, record, content):
        '''Method called for action plugin'''

class Abort(Exception):
    '''Abort the processing for a record'''

class Abort_and_write(Exception):
    '''Abort the processing for a record, then write it in the database'''
    def __init__(self, record={}, *args, **kwargs):
        self.record = record

class Abort_and_update(Exception):
    '''Abort the processing for a record, then write it in the database without updating its timestamp'''
    def __init__(self, record: Record, *_args, **_kwargs):
        self.record = record
