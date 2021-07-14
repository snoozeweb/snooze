#!/usr/bin/python3.6

import os
import yaml
from snooze import __file__ as SNOOZE_PATH
from pathlib import Path
from logging import getLogger
log = getLogger('snooze.utils.config')

SNOOZE_CONFIG_PATH = os.environ.get('SNOOZE_SERVER_CONFIG', '/etc/snooze/server')

def config(configname = 'core', configpath = SNOOZE_CONFIG_PATH):
    '''
    Read a configuration file and return its content.
    '''
    server_config_path = Path(configpath) / (configname + '.yaml')

    default_file = Path(SNOOZE_PATH).parent / 'defaults' / (configname + '.yaml')
    if default_file.is_file():
        return_config = yaml.safe_load(default_file.read_text())
    else:
        log.debug('No default config for %s', default_file)
        return_config = {}

    log.debug('Attempting to load configuration at %s', server_config_path)
    if server_config_path.is_file():
        config = yaml.safe_load(server_config_path.read_text())
        return_config.update(config)
    else:
        log.warning('Could not find config at %s', server_config_path)

    environment_variables = {k:v for (k,v) in os.environ.items() if k.startswith('SNOOZE_SERVER_') and k != 'SNOOZE_SERVER_CONFIG'}
    return_config.update(environment_variables)

    log.debug('Retreived the following config: %s', return_config)

    return return_config

def write_config(configname = 'core', config = {}, configpath = SNOOZE_CONFIG_PATH):
    '''
    Update or create a configuration file.
    '''
    config_file = Path(configpath) / (configname + '.yaml')
    log.debug('Write: Loading configuration from %s', config_file)

    try:
        if config_file.is_file():
            log.debug('Write: %s found', config_file)
            current_config = yaml.safe_load(config_file.read_text())
            current_config.update(config)
        else:
            log.debug('Write: YAML config file not found. Creating %s', config_file)
            current_config = config

        # Write config
        with config_file.open("w") as f:
            yaml.dump(current_config, f)
            log.debug('New config: %s', current_config)
        return {'file': str(config_file)}

    except Exception as e:
        log.error(e)
        return {'error': str(e)}

def get_metadata(plugin_name):
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
