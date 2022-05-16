'''Utils for tasks'''

import platform
from pathlib import Path

import toml

from invoke import task

def get_versions():
    '''Return the current version and release of the project, based on pyproject.toml'''
    pyproject = Path(__file__).parent.parent / 'pyproject.toml'
    mytoml = toml.loads(pyproject.read_text(encoding='utf-8'))
    python_version = mytoml.get('tool', {}).get('poetry', {}).get('version')

    if '+' in python_version: # Development release
        ver, rel = python_version.split('+', 1)
        versions = {
            'global_version': ver,
            'python': python_version,
            'web': f"{ver}-{rel}",
            'rpm': f"{ver}-{rel}",
            'deb': f"{ver}-{rel}",
            'docker': f"{ver}-{rel}",
        }

    else: # Production release
        ver, rel = python_version, None
        versions = {
            'global_version': ver,
            'python': ver,
            'web': ver,
            'rpm': f"{ver}-0",
            'deb': ver,
            'docker': f"{ver}-0",
        }

    return versions

@task(name='version')
def version_task(_ctx, field, github_output=False):
    '''Return the full version for a type of artifact'''
    ver = get_versions()[field]
    if github_output:
        print_github_kv(f"{field}_version", ver)
    else:
        print(ver, end="")

def get_paths(field=None):
    '''Return a directionary of target paths for artifacts'''
    versions = get_versions()
    arch = platform.machine()
    artifacts = {
        'wheel': Path(f"dist/snooze_server-{versions['python']}-py3-none-any.whl"),
        'sdist': Path(f"dist/snooze-server-{versions['python']}.tar.gz"),
        'web': Path(f"dist/snooze-web-{versions['web']}.tar.gz"),
        'docker': Path(f"dist/snooze-server-docker-{versions['docker']}.tar.gz"),
        'rpm': Path(f"dist/RPMS/{arch}/snooze-server-{versions['rpm']}.{arch}.rpm"),
        'deb': Path(f"dist/snooze-server_{versions['deb']}_all.deb"),
        'deb-buildroot': Path(f"snooze-server_{versions['deb']}_{arch}"),
    }
    if field:
        print(artifacts[field], end="")
        return artifacts[field]
    return artifacts

@task(name='path')
def path_task(_ctx, field, github_output=False):
    '''Return a directionary of target path for a given type of artifact'''
    artifacts = get_paths()
    path = artifacts[field]
    if github_output:
        print_github_kv(f"{field}_path", path)
    else:
        print(path, end="")

def git_sanity_check(ctx):
    '''Raise an exception and abort the run if git is not in a clean status'''
    exceptions = ['pyproject.toml', 'web/package.json', 'web/package-lock.json', '']
    lines = ctx.run('git status --porcelain --untracked-files=no').stdout.strip('\r\n').splitlines()
    changes = []
    for line in lines:
        line = line.strip()
        _, path = line.split(' ', 1)
        if path not in exceptions:
            changes.append(path)
    if changes:
        raise Exception(f"Git not in a clean status: {changes}")

def gen_version(ctx):
    '''Generate a dev version number for local use'''
    git_describe = ctx.run('git describe --tags', hide=True).stdout.strip()
    ver, rel = git_describe.split('-', 1)
    ver = ver[1:] # Removing the leading 'v'
    rel = rel.replace('-', '.') # Not supported by rpm
    print(f"Using generated version {ver}+{rel}")
    ctx.run(f"poetry version {ver}+{rel}")
    with ctx.cd('web'):
        ctx.run(f"npm version {ver} --allow-same-version")

def print_github_kv(key, value):
    '''Print output information in github format'''
    print(f"::set-output name={key}::{value}")
