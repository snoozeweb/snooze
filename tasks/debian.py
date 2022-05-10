'''Task to build a deb package for Debian distributions'''

import shutil

from invoke import task, Collection

from tasks.utils import get_versions, print_github_kv, get_paths

@task
def build(ctx, cleanup=True, github_output=False):
    '''Build a deb package'''
    ver_rel = get_versions()['deb']
    artifacts = get_paths()
    target = artifacts['deb']

    buildroot = artifacts['deb-buildroot']
    debian = buildroot / 'DEBIAN'
    venv = buildroot / 'opt' / 'snooze'
    web = venv / 'web'
    etc_server = buildroot / 'etc' / 'snooze' / 'server'
    systemd = buildroot / 'lib' / 'systemd' / 'system'
    varlib = buildroot / 'var' / 'lib' / 'snooze'
    varlog = buildroot / 'var' / 'log' / 'snooze'
    for directory in [buildroot, debian, venv, web, etc_server, systemd, varlib, varlog]:
        directory.mkdir(exist_ok=True, parents=True)

    ctx.run(f"virtualenv --always-copy --python=python3.8 {venv}")
    ctx.run(f"{venv}/bin/pip install {artifacts['wheel']}")
    ctx.run(f"tar -xvf {artifacts['web']} -C {venv}/web")

    ctx.run(f"find '{venv}/lib' -type f -name '*.so' -exec strip {{}} +")
    ctx.run(f"find '{venv}' -name '*.py' -exec sed -i 's+^#\\!/.*$+#!/opt/snooze/bin/python3 -s+g' {{}} +")
    ctx.run(f"find '{venv}/bin' -maxdepth 1 -type f -exec sed -i 's+^#\\!/.*$+#!/opt/snooze/bin/python3 -s+g' {{}} +")
    ctx.run(f"find '{buildroot}' -name 'RECORD' -exec rm -rf {{}} +")
    ctx.run(f"find '{venv}/lib' -type f -name '*.so' -exec strip {{}} +")

    ctx.run(f"cp -r debian/* {debian}")
    ctx.run(f"sed -i 's/__VERSION__/{ver_rel}/' {debian}/control")

    ctx.run(f"cp packaging/files/core.yaml {etc_server}")
    ctx.run(f"cp packaging/files/logging.yaml {etc_server}/logging.yaml")
    ctx.run(f"cp packaging/files/snooze-server.service {systemd}")

    ctx.run(f"dpkg-deb --build {buildroot} dist")

    if cleanup:
        shutil.rmtree(buildroot)

    if github_output:
        print_github_kv('PATH', target)
        print_github_kv('ASSET_NAME', target.name)

ns = Collection('debian')
ns.add_task(build)
