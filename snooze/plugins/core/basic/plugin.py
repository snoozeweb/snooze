#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

import sys
from logging import getLogger
from os.path import dirname, join as joindir
from abc import abstractmethod
from pathlib import Path
from typing import Optional, Dict

from pydantic import BaseModel

from snooze import __file__ as rootdir
from snooze.utils.config import MetadataConfig
from snooze.utils.typing import Record, RouteArgs

log = getLogger('snooze')


class Metadata(BaseModel):
    '''Object representing the live metadata of a plugin.
    Basically MetadataConfig with computed routes.'''
    name: str
    auto_reload: bool
    default_sorting: Optional[str]
    default_ordering: bool
    widgets: dict
    action_form: dict
    audit: bool
    routes: Dict[str, RouteArgs]
    route_defaults: RouteArgs
    options: dict
    permissions: list

class Plugin:
    def __init__(self, core: 'Core'):
        self.core = core
        self.db = core.db
        self.name = self.__class__.__name__.lower()
        self.data = []
        self.rootdir = joindir(dirname(rootdir), 'plugins', 'core', self.name)
        moduledir = Path(sys.modules[self.__module__].__file__)
        config = MetadataConfig(self.name, moduledir)
        routes = {}
        default_routes = [
            f"/{self.name}",
            f"/{self.name}" + '/{search}',
            f"/{self.name}" + '/{search}/{perpage}/{pagenb}',
            f"/{self.name}" + '/{search}/{perpage}/{pagenb}/{orderby}/{asc}',
        ]
        for path in default_routes:
            routes[path] = config.route_defaults
        if config.routes:
            routes.update(config.routes)

        if config.action_form:
            config.action_form['action_name'] = self.name
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
            action_form=config.action_form,
            routes=routes,
            route_defaults=config.route_defaults,
            audit=config.audit,
            options=config.options,
            permissions=config.provides,
        )
        if config.search_fields:
            self.db.create_index(self.name, config.search_fields)

    def validate(self, obj: dict):
        '''Validate an object before writing it to the database.
        Should raise an exception if the object is invalid
        '''

    def post_init(self):
        '''Hook to execute something after the default init'''
        self.reload_data()

    def reload_data(self, sync: bool = False):
        '''Reload the data of a plugin from the database'''
        if self.meta.auto_reload:
            log.debug("Reloading data for plugin %s", self.name)
            pagination = {}
            if self.meta.default_sorting is not None:
                pagination['orderby'] = self.meta.default_sorting
            pagination['asc'] = self.meta.default_ordering
            self.data = self.db.search(self.name, **pagination)['data']
        if sync:
            self.sync_neighbors()

    def sync_neighbors(self):
        '''Trigger the reload of the module to neighbors (async)'''
        self.core.sync_reload_plugin(self.name)

    def process(self, record: Record) -> Record:
        '''Process a record if it's a process plugin'''
        return record

    def pprint(self, options: dict = {}) -> str:
        return self.name

    @abstractmethod
    def send(self, record, content):
        '''Method called for action plugin'''

class Abort(Exception):
    '''Abort the processing for a record'''

class AbortAndWrite(Exception):
    '''Abort the processing for a record, then write it in the database'''
    def __init__(self, record={}, *args, **kwargs):
        self.record = record

class AbortAndUpdate(Exception):
    '''Abort the processing for a record, then write it in the database without updating its timestamp'''
    def __init__(self, record: Record, *_args, **_kwargs):
        self.record = record
