#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''Module for managing loading and writing the configuration files'''

import os
from logging import getLogger
from pathlib import Path
from typing import Optional

import yaml

from snooze import __file__ as SNOOZE_PATH
from snooze.utils.typing import Config

log = getLogger('snooze.utils.config')

SNOOZE_CONFIG_PATH = os.environ.get('SNOOZE_SERVER_CONFIG', '/etc/snooze/server')

def config(configname: str = 'core', configpath: str = SNOOZE_CONFIG_PATH) -> Config:
    '''Read a configuration file and return its content'''
    server_config_path = Path(configpath) / (configname + '.yaml')

    default_file = Path(SNOOZE_PATH).parent / 'defaults' / (configname + '.yaml')
    if default_file.is_file():
        return_config = yaml.safe_load(default_file.read_text())
    else:
        log.debug('No default config for %s', default_file)
        return_config = {}

    log.debug('Attempting to load configuration at %s', server_config_path)
    if server_config_path.is_file():
        config_dict = yaml.safe_load(server_config_path.read_text())
        return_config.update(config_dict)
    else:
        log.warning('Could not find config at %s', server_config_path)

    environment_variables = {
        key: value
        for (key, value) in os.environ.items()
        if key.startswith('SNOOZE_SERVER_') and key != 'SNOOZE_SERVER_CONFIG'
    }
    return_config.update(environment_variables)

    log.debug('Retreived the following config: %s', return_config)

    return return_config

def write_config(config_name: str = 'core', config_dict: Optional[dict] = None, config_path=SNOOZE_CONFIG_PATH) -> dict:
    '''Update or create a configuration file'''
    if config_dict is None:
        config_dict = {}
    config_file = Path(config_path) / (config_name + '.yaml')
    log.debug('Write: Loading configuration from %s', config_file)

    try:
        if config_file.is_file():
            log.debug('Write: %s found', config_file)
            current_config = yaml.safe_load(config_file.read_text())
            current_config.update(config_dict)
        else:
            log.debug('Write: YAML config file not found. Creating %s', config_file)
            current_config = config_dict

        # Write config
        with config_file.open("w") as config_fd:
            yaml.dump(current_config, config_fd)
            log.debug('New config: %s', current_config)
        return {'file': str(config_file)}

    except Exception as err:
        log.error(err)
        return {'error': str(err)}

def get_metadata(plugin_name: str) -> dict:
    '''Read metadata at a given plugin path'''
    plugin_root = Path(SNOOZE_PATH).parent / 'plugins/core'
    metadata_path = plugin_root / plugin_name / 'metadata.yaml'
    try:
        log.debug("Attempting to read metadata at %s for %s module", metadata_path, plugin_name)
        data = metadata_path.read_text()
        metadata = yaml.safe_load(data)
        return metadata
    except Exception as err:
        log.warning("Skipping. Cannot read %s due to: %s", metadata_path, err)
        return {}
