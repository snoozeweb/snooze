#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

import sys
from logging import getLogger
from os.path import dirname, join as joindir
from datetime import datetime
from abc import abstractmethod
from pathlib import Path
from typing import Optional, Dict
from uuid import uuid4
from dataclasses import dataclass

from pydantic import BaseModel

from snooze import __file__ as SNOOZE_PATH
from snooze.utils.config import MetadataConfig
from snooze.utils.typing import Record, RouteArgs

log = getLogger('snooze')
apilog = getLogger('snooze-api')

SNOOZE_PLUGIN_PATH = Path(SNOOZE_PATH).parent / 'plugins/core'

class Metadata(BaseModel):
    '''Object representing the live metadata of a plugin.
    Basically MetadataConfig with computed routes.'''
    name: str
    auto_reload: bool
    default_sorting: Optional[str]
    default_ordering: bool
    widgets: dict
    icon: str
    action_name: Optional[str]
    action_form: dict
    audit: bool
    routes: Dict[str, RouteArgs]
    route_defaults: RouteArgs
    options: dict
    permissions: list
    force_order: Optional[str]
    tree: bool

class Plugin:
    def __init__(self, core: 'Core'):
        self.core = core
        self.db = core.db
        self.name = self.__class__.__name__.lower()
        self.data = []
        self.hostname = core.config.syncer.hostname

        pkgdir = Path(sys.modules[self.__module__].__file__).parent

        if (SNOOZE_PLUGIN_PATH / self.name / 'metadata.yaml').is_file():
            moduledir = SNOOZE_PLUGIN_PATH / self.name
        elif (pkgdir / 'metadata.yaml').is_file():
            moduledir = pkgdir
        else:
            apilog.warning("No metadata found for plugin '%s'", self.name)
            moduledir = None
        config = MetadataConfig(self.name, moduledir)
        self.rootdir = moduledir
        routes = {}
        if config.route_defaults.class_name:
            default_routes = [
                f"/{self.name}",
                f"/{self.name}" + '/{search}',
                f"/{self.name}" + '/{search}/{perpage}/{pagenb}',
                f"/{self.name}" + '/{search}/{perpage}/{pagenb}/{orderby}/{asc}',
            ]
            for path in default_routes:
                routes[path] = config.route_defaults
        for path, route in config.routes.items():
            routes[path] = config.route_defaults.merge(route)

        action_name = None
        if config.action_form:
            action_name = self.name
            batch = config.options.get('batch')
            if batch and not batch.get('hidden', False):
                batch_form = {
                    'batch': {
                        'display_name': 'Batch',
                        'component': 'Switch',
                        'default': batch.get('default', False),
                        'description': 'Batch alerts',
                    },
                    'batch_timer': {
                        'display_name': 'Batch Timer',
                        'component': 'Duration',
                        'description': 'Number of seconds to wait before sending a batch',
                        'options': {
                            'zero_label': 'Immediate',
                            'negative_label': 'Immediate',
                        },
                        'default_value': batch.get('timer', 10),
                    },
                    'batch_maxsize': {
                        'display_name': 'Batch Maxsize',
                        'component': 'Number',
                        'description': 'Maximum batch size to send',
                        'options': {
                            'min': 1,
                        },
                        'default_value': batch.get('maxsize', 100),
                    },
                }
                config.action_form.update(batch_form)
        self.meta = Metadata(
            name=self.name,
            auto_reload=config.auto_reload,
            default_sorting=config.default_sorting,
            default_ordering=config.default_ordering,
            widgets=config.widgets,
            icon=config.icon,
            action_name=action_name,
            action_form=config.action_form,
            routes=routes,
            route_defaults=config.route_defaults,
            audit=config.audit,
            options=config.options,
            permissions=config.provides,
            force_order=config.force_order,
            tree=config.tree,
        )
        if config.search_fields:
            self.db.create_index(self.name, config.search_fields)

        # Bootstrap script for syncer_latest
        now = datetime.now().timestamp()
        if not self.db.get_one('syncer_latest', dict(type='plugin', name=self.name)):
            self.db.replace_one('syncer_latest', dict(type='plugin', name=self.name), {
                'uid': str(uuid4()),
                'node': self.hostname,
                'type': 'plugin',
                'name': self.name,
                'timestamp': now
            })

    def validate(self, obj: dict):
        '''Validate an object before writing it to the database.
        Should raise an exception if the object is invalid
        '''

    def post_init(self):
        '''Hook to execute something after the default init'''
        self.reload_data()

    def _post_reload(self):
        '''Hook to execute action after the standard data reload'''

    def reload_data(self):
        '''Reload the data of a plugin from the database'''
        if self.meta.auto_reload:
            log.info("Reloading plugin '%s'...", self.name)
            pagination = {}
            if self.meta.default_sorting is not None:
                pagination['orderby'] = self.meta.default_sorting
            pagination['asc'] = self.meta.default_ordering
            self.data = self.db.search(self.name, **pagination)['data']

            # Update the syncer with the node's value
            now = datetime.now().timestamp()
            self.db.replace_one('syncer_node', dict(node=self.hostname, type='plugin', name=self.name), {
                'node': self.hostname,
                'type': 'plugin',
                'name': self.name,
                'timestamp': now,
            })
            self._post_reload()
            log.info("Reloaded plugin '%s'", self.name)

    def process(self, record: Record) -> Record:
        '''Process a record if it's a process plugin'''
        return record

    def pprint(self, options: dict = {}) -> str:
        return self.name

    @abstractmethod
    def send(self, record, content):
        '''Method called for action plugin'''

class Abort:
    '''Abort the processing for a record'''

@dataclass
class AbortAndWrite:
    '''Abort the processing for a record, then write it in the database'''
    record: dict

@dataclass
class AbortAndUpdate:
    '''Abort the processing for a record, then write it in the database without updating its timestamp'''
    record: dict
