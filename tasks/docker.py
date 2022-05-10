'''Docker related tasks'''

import io
import os
from getpass import getpass

from invoke import task, Collection

from tasks.utils import get_versions, print_github_kv, get_paths

def docker_images(ctx, tag):
    '''Check if an image was already built'''
    cmd = ' '.join(['docker', 'images', tag, '--format', '"{{ .ID }}"'])
    images = ctx.run(cmd, hide=True).stdout.splitlines()
    return images

@task
def check(ctx, ignore_check=('DL3008', 'DL3018', 'DL3059'), dockerfile="packaging/Dockerfile"):
    '''Check the validity of the Dockerfile'''
    ignores = ' '.join([f"--ignore {i}" for i in ignore_check])
    ctx.run(f"docker run --rm -i ghcr.io/hadolint/hadolint hadolint {ignores} - < {dockerfile}")

def version():
    '''Return the version of the docker container'''
    return get_versions()['docker'].split('-', 1)[0]

def release():
    '''Return the release number of the docker container'''
    return get_versions()['docker'].split('-', 1)[-1]

@task(help={'mode': 'The mode to auto generate the tags'})
def build(ctx, mode='dev', save=False, github_output=False, dockerfile="packaging/Dockerfile"):
    '''Build a docker image based on the latest git version'''
    image = ctx.get('image')
    repo = ctx.get('repo')
    ca_path = ctx.get('ca_path')
    ver = version()
    rel = release()
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
    artifacts = get_paths()
    _tags = ' '.join([f"-t {repo}/{image}:{tag}" for tag in tags])
    cmd = [
        'docker build',
        f"-f {dockerfile}",
        f"--build-arg VERSION={ver}",
        f"--build-arg RELEASE={rel}",
        f"--build-arg VCS_REF={ref}",
        f"--build-arg WEB_PATH={artifacts['web']}",
        f"--build-arg WHEEL_PATH={artifacts['wheel']}",
        f"{_tags}",
        '.',
    ]
    if ca_path:
        cmd.append(f"-v {ca_path}:/usr/local/share/ca-certificates:ro")
    ctx.run(' '.join(cmd))
    if save:
        target = artifacts['docker']
        ctx.run(f"docker save {repo}/{image}:{ver_rel} | gzip > {target}")
        print(f"Docker image saved at {target}")
        if github_output:
            print_github_kv('PATH', target)

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
    ver = version()
    rel = release()
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
ns.add_task(check)
ns.add_task(build)
ns.add_task(push)
