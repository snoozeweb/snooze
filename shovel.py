'''Project tasks'''

import os
import json
import sys

from subprocess import Popen, PIPE

from shovel import task

PWD = os.getcwd()

def execute(*cmd):
    '''Wrapper to execute a subprocess'''
    cmd_string = " ".join(cmd)
    print("Running {}".format(cmd_string))
    proc = Popen(cmd, stdout=PIPE, stderr=PIPE)
    outs, errs = proc.communicate()
    if proc.returncode == 0:
        print("Command `{}` successful: {}".format(cmd_string, outs))
        return outs.decode()
    else:
        print("Command `{}` failed with error {}: (stdout) `{}` / (stderr) `{}`".format(cmd_string, proc.returncode, outs, errs))
        sys.exit(1)

def get_version():
    version_path = '{}/VERSION'.format(os.environ['PWD'])
    if os.path.isfile(version_path):
        with open(version_path) as f:
            version = f.read().rstrip('\n')

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
def docker():
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
