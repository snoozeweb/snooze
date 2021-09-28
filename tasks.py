'''Project tasks'''

import os
import json
import sys

from subprocess import Popen, PIPE
from getpass import getpass

import toml
from pathlib import Path
from invoke import task

PWD = os.getcwd()

def get_version():
    '''Returns the version specified in pyproject.toml'''
    pyproject = Path(__file__).parent / 'pyproject.toml'
    mytoml = toml.loads(pyproject.read_text(encoding='utf-8'))
    return mytoml.get('tool', {}).get('poetry', {}).get('version')

@task
def version(c):
    '''Return the version number of pyproject.toml'''
    print(get_version(), end="")

@task
def build_vue(c):
    print("Building vue package")
    c.run("npm install --only=production")

@task
def rpm(c):
    print("Building rpm")
    c.run('rpmbuild --bb snooze-server.spec')

@task
def docker_build(c):
    print("Building docker image")
    repo_url = 'registry.hub.docker.com'
    image_name = 'snoozeweb/snooze'
    vcs_ref = execute('git', 'rev-parse', '--short', 'HEAD').rstrip('\r\n')
    version = get_version()
    cmd = [
        'docker', 'build',
        '--build-arg', 'VCS_REF={}'.format(vcs_ref),
        '--build-arg', 'VERSION={}'.format(version),
        '-t', '{}/{}:v{}'.format(repo_url, image_name, version),
        '-t', '{}/{}:{}'.format(repo_url, image_name, vcs_ref),
        '-t', '{}/{}:latest'.format(repo_url, image_name),
        '.',
    ]
    c.run(' '.join(cmd))

@task
def docker_push(c):
    print("Pushing docker image")
    password = getpass()
    repo_url = 'registry.hub.docker.com'
    image_name = 'snoozeweb/snooze'
    vcs_ref = execute('git', 'rev-parse', '--short', 'HEAD').rstrip('\r\n')
    version = get_version()
    c.run("docker login {} --username snoozeweb --password {}".format(repo_url, password))
    c.run("docker push {}/{}:v{}".format(repo_url, image_name, version))
    c.run("docker push {}/{}:{}".format(repo_url, image_name, vcs_ref))
    c.run("docker push {}/{}:latest".format(repo_url, image_name))
