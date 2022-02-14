'''Docker related tasks'''

import io
import os
from getpass import getpass

from invoke import task, Collection

from tasks.utils import get_version

def docker_images(ctx, tag):
    '''Check if an image was already built'''
    cmd = ' '.join(['docker', 'images', tag, '--format', '"{{ .ID }}"'])
    images = ctx.run(cmd, hide=True).stdout.splitlines()
    return images

@task
def check(ctx, ignore_check=('DL3008', 'DL3018', 'DL3059'), dockerfile="Dockerfile-local"):
    '''Check the validity of the Dockerfile'''
    ignores = ' '.join([f"--ignore {i}" for i in ignore_check])
    ctx.run(f"docker run --rm -i ghcr.io/hadolint/hadolint hadolint {ignores} < {dockerfile}")

@task(help={'mode': 'The mode to auto generate the tags'})
def build(ctx, mode='dev'):
    '''Build a docker image based on the latest git version'''
    image = ctx.get('image')
    repo = ctx.get('repo')
    ca_path = ctx.get('ca_path')
    ver, rel = get_version()
    ver_rel = f"{ver}-{rel}"
    if mode == 'dev':
        tags = [ver_rel]
    elif mode == 'production':
        tags = ['latest', ver, ver_rel]
    else:
        raise Exception(f"Unknown mode {mode}")
    ref = ctx.run('git rev-parse --short HEAD', hide=True).stdout.strip('\r\n')

    images = docker_images(ctx, f"{repo}/{image}:{ver_rel}")
    if images:
        print(f"Image {repo}/{image}:{ver_rel} already exist at: {images}")
        return
    print("Building docker image")
    _tags = ' '.join([f"-t {repo}/{image}:{tag}" for tag in tags])
    cmd = [
        'docker build',
        '-f Dockerfile-local',
        f"--build-arg VERSION={ver}",
        f"--build-arg RELEASE={rel}",
        f"--build-arg VCS_REF={ref}",
        f"{_tags}",
        '.',
    ]
    if ca_path:
        cmd.append(f"-v {ca_path}:/usr/local/share/ca-certificates:ro")
    ctx.run(' '.join(cmd))

@task(help={'mode': 'The mode to auto generate the tags'})
def push(ctx, username=None, password=None, mode='dev'):
    '''Perform a docker push to a given repo'''
    image = ctx.get('image')
    repo = ctx.get('repo')
    print("Pushing docker image")
    if username is None:
        username = os.environ.get('USER')
    if password is None:
        password = getpass()
    ver, rel = get_version()
    ver_rel = f"{ver}-{rel}"
    if mode == 'dev':
        tags = [ver_rel]
    elif mode == 'production':
        tags = ['latest', ver, ver_rel]
    else:
        raise Exception(f"Unknown mode {mode}")
    ctx.run(f"docker login {repo} --username {username} --password-stdin", in_stream=io.StringIO(password))
    for tag in tags:
        ctx.run(f"docker push {repo}/{image}:{tag}")

config = {
    'image': 'snoozeweb/snooze',
    'repo': 'docker.io',
}

ns = Collection('docker')
ns.configure(config)
ns.add_task(build)
ns.add_task(push)
