'''Tasks related to the web interface'''

from pathlib import Path

from invoke import task, Collection

from tasks.utils import get_version, print_github_kv

@task
def build(ctx, force=False, github_output=False):
    '''Build and package the web interface javascript'''
    ver, rel = get_version()
    ver_rel = f"{ver}-{rel}" if rel else ver
    target = Path(f"dist/snooze-web-{ver_rel}.tar.gz")
    if (not force) and target.exists():
        print(f"Target {target} already exists")
        return
    print("Cleaning up directory")
    with ctx.cd('web/'):
        ctx.run('rm -rf dist')
        ctx.run('mkdir dist')
    print("Building vue package")
    with ctx.cd('web'):
        ctx.run('npm ci')
        ctx.run('npm run build')
    ctx.run(f"tar -C ./web/dist/ -czf {target} .")
    ctx.run('rm -rf ./web/dist/')
    if github_output:
        print_github_kv('ASSET_NAME', target.name)
        print_github_kv('PATH', target)

ns = Collection('web')
ns.add_task(build)
