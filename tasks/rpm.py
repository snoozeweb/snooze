'''Build the rpm'''

import platform
from pathlib import Path
from tempfile import TemporaryDirectory

from invoke import task, Collection

import tasks.pip
import tasks.web
from tasks.utils import get_version, print_github_kv

@task(tasks.pip.build, tasks.web.build)
def build(ctx, force=False, github_output=False):
    '''Build a rpm based on the code produce by pip and vue build jobs'''
    ver, rel = get_version()
    arch = platform.machine()
    target = Path(f"dist/RPMS/{arch}/snooze-server-{ver}-{rel}.{arch}.rpm")
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
        ctx.run(f"rpmbuild --define '_topdir {tmpdir}' --ba snooze-server-local.spec -vv --debug")
        ctx.run(f"cp -r {tmpdir}/RPMS/ dist/")
    if github_output:
        print_github_kv('PATH', target)
        print_github_kv('ASSET_NAME', target.name)

ns = Collection('rpm')
ns.add_task(build)
