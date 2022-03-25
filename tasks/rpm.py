'''Build the rpm'''

from pathlib import Path
from tempfile import TemporaryDirectory

from invoke import task, Collection

import tasks.pip
import tasks.web
from tasks.utils import print_github_kv, get_paths, get_versions

@task(tasks.pip.build, tasks.web.build)
def build(ctx, force=False, github_output=False):
    '''Build a rpm based on the code produce by pip and vue build jobs'''
    artifacts = get_paths()
    target = artifacts['rpm']
    if (not force) and target.exists():
        print(f"Target {target} already exists")
        return
    print("Building rpm")
    with TemporaryDirectory(prefix='snooze-rpm-build') as tmpdir:
        basedir = Path(tmpdir)
        for name in ['BUILD', 'SOURCES', 'SRPMS', 'RPMS']:
            (basedir / name).mkdir()
        ctx.run(f"cp -r dist/* {tmpdir}/SOURCES/")
        ctx.run(f"cp snooze-server.service {tmpdir}/SOURCES/")
        ctx.run(f"cp snooze/defaults/core.yaml {tmpdir}/SOURCES/")
        ctx.run(f"cp snooze/defaults/logging_file.yaml {tmpdir}/SOURCES/logging.yaml")
        ctx.run(f"rpmbuild --define '_topdir {tmpdir}' --ba snooze-server-local.spec -vv --debug")
        ctx.run(f"cp -r {tmpdir}/RPMS/ dist/")
    if github_output:
        print_github_kv('PATH', target)
        print_github_kv('ASSET_NAME', target.name)

@task
def version(_ctx):
    '''Return the rpm version'''
    ver, _ = get_versions()['rpm'].split('-', 1)
    print(ver, end="")

@task
def release(_ctx):
    '''Return the rpm release'''
    _, rel = get_versions()['rpm'].split('-', 1)
    print(rel, end="")

ns = Collection('rpm')
ns.add_task(build)
ns.add_task(version)
ns.add_task(release)
