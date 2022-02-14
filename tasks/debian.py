'''Task to build a deb package for Debian distributions'''

import os
import shutil
import platform
from pathlib import Path

from invoke import task, Collection

from tasks.utils import get_version

@task
def build(ctx, cleanup=True, github_output=False):
    '''Build a deb package'''
    ver, rel = get_version()
    ver_rel = f"{ver}-{rel}" if rel else ver
    pip_ver_rel = f"{ver}+{rel}" if rel else ver
    arch = platform.machine()
    target = Path(f"dist/snooze-server_{ver_rel}_{arch}.deb")

    buildroot = Path(f"snooze-server_{ver_rel}_{arch}")
    debian = buildroot / 'DEBIAN'
    venv = buildroot / 'opt' / 'snooze'
    web = venv / 'web'
    etc_server = buildroot / 'etc' / 'snooze' / 'server'
    systemd = buildroot / 'lib' / 'systemd' / 'system'
    for directory in [buildroot, debian, venv, web, etc_server, systemd]:
        directory.mkdir(exist_ok=True, parents=True)

    ctx.run(f"virtualenv --always-copy --python=python3.8 {venv}")
    ctx.run(f"{venv}/bin/pip install dist/snooze_server-{pip_ver_rel}-py3-none-any.whl")
    ctx.run(f"tar -xvf dist/snooze-web-{ver_rel}.tar.gz -C {venv}/web")

    ctx.run(f"find '{venv}/lib' -type f -name '*.so' -exec strip {{}} +")
    ctx.run(f"find '{venv}' -name '*.py' -exec sed -i 's+^#\\!/.*$+\\!/opt/snooze/bin/python3 -s+g' {{}} +")
    ctx.run(f"find '{venv}/bin' -maxdepth 1 -type f -exec sed -i 's+^#\\!/.*$+#\\!/opt/snooze/bin/python3 -s+g' {{}} +")

    ctx.run(f"cp -r debian/* {debian}")
    ctx.run(f"sed -i 's/__VERSION__/{ver_rel}/' {debian}/control")

    ctx.run(f"cp snooze/defaults/core.yaml {etc_server}")
    ctx.run(f"cp snooze-server.service {systemd}")

    ctx.run(f"dpkg-deb --build {buildroot} dist")

    if cleanup:
        shutil.rmtree(buildroot)

    if github_output:
        print_github_kv('PATH', target)
        print_github_kv('ASSET_NAME', target.name)

ns = Collection('debian')
ns.add_task(build)
