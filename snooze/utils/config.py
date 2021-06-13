#!/usr/bin/python3.6

import os
import yaml
from snooze import __file__ as SNOOZE_PATH
from pathlib import Path
from logging import getLogger
log = getLogger('snooze.utils.config')

SNOOZE_CONFIG_PATH = os.environ.get('SNOOZE_CONFIG_PATH')

DEFAULT_PATHS = [
    Path('.'),
    Path('/etc/snooze'),
    Path(SNOOZE_PATH).parent / 'utils',
]

if SNOOZE_CONFIG_PATH:
    DEFAULT_PATHS.append(SNOOZE_CONFIG_PATH)

def config(configname = 'config', use_env = True):
    paths = [path for path in DEFAULT_PATHS if path.is_dir()]
    log.debug('Read: Loading configuration from {}'.format(paths))
    return_config = {}
    for filename in ['default_' + configname + '.yaml', configname + '.yaml']:
        for path in paths:
            configfile = path / filename
            if configfile.is_file():
                log.debug('Read: Found YAML config file at {}'.format(configfile))
                with configfile.open('r') as f:
                    try:
                        for y in yaml.load_all(f.read()):
                            return_config.update(y)
                    except Exception as e:
                        log.error(e)
                        return {'error': str(e)}
    if use_env:
        environment_variables = {k:v for (k,v) in os.environ.items() if k.startswith('SNOOZE_') and k != 'SNOOZE_CONFIG_PATH'}
        return_config.update(environment_variables)
    log.debug('Will load the following config: {}'.format(return_config))
    return return_config

def write_config(configname = 'config', config = {}):
    paths = [path for path in DEFAULT_PATHS if path.is_dir()]
    log.debug('Write: Loading configuration from {}'.format(paths))
    configfile = ''
    found_file = False
    for path in paths:
        configfile = path / (configname + '.yaml')
        if configfile.is_file():
            log.debug('Write: Found YAML config file at {}'.format(configfile))
            found_file = True
            break
    if not found_file:
        configfile = paths[-1] / (configname + '.yaml')
        log.debug('Write: YAML config file not found. Creating {}'.format(configfile))
    try:
        with configfile.open('r') as f:
            old_config = yaml.load(f.read(), Loader=yaml.Loader)
            log.debug('Writing config {}'.format(config))
            log.debug('Type {}'.format(type(config)))
            old_config.update(config)
        with configfile.open('w') as f:
            yaml.dump(old_config, f)
            log.debug('New config: {}'.format(old_config))
            return {'file': str(configfile)}
    except Exception as e:
        log.error(e)
        return {'error': str(e)}
