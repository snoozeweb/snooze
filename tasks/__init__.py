'''Project tasks'''

import io
import re
import platform

from invoke import task, Collection

from tasks import debian, docker, rpm, pip, web
from tasks.utils import *

@task
def version(_ctx, github_output=False):
    '''Return the version number of pyproject.toml'''
    ver = get_version()[0]
    if github_output:
        print_github_kv('VERSION', ver)
    else:
        print(ver, end="")

@task
def release(_ctx, github_output=False):
    '''Return a release number corresponding to the state of the git'''
    rel = get_version()[1]
    if github_output:
        print_github_kv('RELEASE', rel)
    else:
        print(rel, end="")

@task
def changelog(_ctx, github_output=False):
    '''Print the changelog of the latest version'''
    path = Path('CHANGELOG.md')
    latest_logs = re.split('^(## .*)\n', path.read_text(encoding='utf-8'), flags=re.MULTILINE)
    print(latest_logs[1])
    print(latest_logs[2])
    output = latest_logs[2]
    if github_output:
        output = output.replace('%', '%25')
        output = output.replace('\n', '%0A')
        output = output.replace('\r', '%0D')
        print_github_kv('CHANGELOG', output)

@task
def dev_build(ctx, force=False):
    '''Build several packages for development purposes (use a dev versioning)'''
    git_sanity_check(ctx)
    gen_version(ctx)
    web.build(ctx, force=force)
    pip.build(ctx, force=force)
    rpm.build(ctx, force=force)

config = {
    'run': {
        'echo': True,
    },
}

ns = Collection()
ns.configure(config)
ns.add_collection(docker.ns)
ns.add_collection(web.ns)
ns.add_collection(rpm.ns)
ns.add_collection(pip.ns)
ns.add_collection(debian.ns)

ns.add_task(version)
ns.add_task(release)
ns.add_task(dev_build)
ns.add_task(changelog)
