'''Utils for tasks'''

import toml
from pathlib import Path

def get_version():
    '''Return the current version and release of the project'''
    pyproject = Path(__file__).parent.parent / 'pyproject.toml'
    mytoml = toml.loads(pyproject.read_text(encoding='utf-8'))
    ver = mytoml.get('tool', {}).get('poetry', {}).get('version')
    return ver.split('+', 1)

def git_sanity_check(ctx):
    '''Raise an exception and abort the run if git is not in a clean status'''
    exceptions = ['pyproject.toml', 'web/package.json', '']
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
