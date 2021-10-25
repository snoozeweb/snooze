#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''Project tasks'''

import os
import json
import sys

from subprocess import Popen, PIPE
from getpass import getpass

from shovel import task

PWD = os.getcwd()

def execute(*cmd, show_stdout = True):
    '''Wrapper to execute a subprocess'''
    cmd_string = " ".join(cmd)
    if show_stdout:
        print("Running {}".format(cmd_string))
    proc = Popen(cmd, stdout=PIPE, stderr=PIPE)
    outs, errs = proc.communicate()
    if proc.returncode == 0:
        if show_stdout:
            print("Command `{}` successful: {}".format(cmd_string, outs))
        else:
            print("Command successful: {}".format(cmd_string, outs))
        return outs.decode()
    else:
        if show_stdout:
            print("Command `{}` failed with error {}: (stdout) `{}` / (stderr) `{}`".format(cmd_string, proc.returncode, outs, errs))
        else:
            print("Command failed with error {}: (stdout) `{}` / (stderr) `{}`".format(proc.returncode, outs, errs))
        sys.exit(1)

def get_version():
    version_path = '{}/VERSION'.format(os.environ['PWD'])
    if os.path.isfile(version_path):
        with open(version_path) as f:
            version = f.read().rstrip('\n')
    return version

@task
def build_vue():
    print("Building vue package")
    execute("npm", "install", "--only=production")

@task
def pipenv():
    print("Running pipenv to pull dependencies")
    execute('pipenv', 'install', '--deploy', '--ignore-pipfile')
    execute('pipenv', 'clean')

@task
def rpm():
    print("Building rpm")
    execute('rpmbuild', '--bb', 'snooze-server.spec')

@task
def docker_build():
    print("Building docker image")
    repo_url = 'registry.hub.docker.com'
    image_name = 'snoozeweb/snooze'
    vcs_ref = execute('git', 'rev-parse', '--short', 'HEAD').rstrip('\r\n')
    version = get_version()
    execute('docker', 'build',
            '--build-arg', 'VCS_REF={}'.format(vcs_ref),
            '--build-arg', 'VERSION={}'.format(version),
            '-t', '{}/{}:v{}'.format(repo_url, image_name, version),
            '-t', '{}/{}:{}'.format(repo_url, image_name, vcs_ref),
            '-t', '{}/{}:latest'.format(repo_url, image_name),
            '.')

@task
def docker_push():
    print("Pushing docker image")
    password = getpass()
    repo_url = 'registry.hub.docker.com'
    image_name = 'snoozeweb/snooze'
    vcs_ref = execute('git', 'rev-parse', '--short', 'HEAD').rstrip('\r\n')
    version = get_version()
    execute('docker', 'login',
            '{}'.format(repo_url),
            '--username', 'snoozeweb',
            '--password', '{}'.format(password),
            show_stdout=False)
    execute('docker', 'push',
            '{}/{}:v{}'.format(repo_url, image_name, version))
    execute('docker', 'push',
            '{}/{}:{}'.format(repo_url, image_name, vcs_ref))
    execute('docker', 'push',
            '{}/{}:latest'.format(repo_url, image_name))
