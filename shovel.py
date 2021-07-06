'''Project tasks'''

import os
import json
import sys

from subprocess import Popen

from shovel import task

PWD = os.getcwd()

def execute(*cmd):
    '''Wrapper to execute a subprocess'''
    cmd_string = " ".join(cmd)
    print("Running {}".format(cmd_string))
    proc = Popen(cmd)
    return_code = proc.wait()
    if return_code == 0:
        print("Command successful: {}".format(cmd_string))
    else:
        print("Command failed: `{}` exited with {}".format(cmd_string, return_code))
        sys.exit(1)

@task
def build_vue():
    print("Building vue package")
    execute("npm", "install", "--only=production")

@task
def pipenv():
    print("Running pipenv to pull dependencies")
    execute('pipenv', 'install', '--deploy', '--ignore-pipfile')
    execute('pipenv', 'clean')

def ensure_rpmmacros_line(myline):
    '''
    Ensure the user's .rpmmacros contains `%_build_id_links none`.
    This is necessary in CentOS 8, since it creates /usr/lib/.build-id/ files
    which actually conflict with other rpms since we're actually shipping the same
    binaries (we're shipping python).
    Since .rpmmacros cannot be defined per-project but rather per-user, and we cannot
    customize it with rpmvenv library, the workaround is to ensure the user that build
    the program has it.
    '''
    print("Checking ~/.rpmmacros for line '{}'".format(myline))
    rpmmacros = '{}/.rpmmacros'.format(os.environ['HOME'])
    if os.path.isfile(rpmmacros):
        with open(rpmmacros, 'r+') as f:
            if not any(line.rstrip('\r\n') == myline for line in f.readlines()):
                f.write(myline + '\n')
    else:
        with open(rpmmacros, 'w') as f:
            f.write(myline + '\n')

@task
def rpm():
    ensure_rpmmacros_line('%_build_id_links none')
    print("Building rpm")
    with open('rpmvenv.json') as f:
        config = json.loads(f.read())
    core = config.get('core', {})
    name = core.get('name')
    version = core.get('version')
    release = core.get('release', '1')
    arch = core.get('arch', 'x86_64')
    pkg_name = "{}-{}-{}.{}.rpm".format(name, version, release, arch)
    if os.path.isfile('dist/' + pkg_name):
        print("Detected dist/{}. Deleting.".format(pkg_name))
        os.remove('dist/' + pkg_name)
    execute('rpmvenv', '--verbose', 'rpmvenv.json', '--destination', 'dist/')
