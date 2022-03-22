'''Tasks related to the web interface'''

from pathlib import Path

from invoke import task, Collection

from tasks.utils import print_github_kv, get_paths

@task
def build(ctx, force=False, github_output=False):
    '''Build and package the web interface javascript'''
    artifacts = get_paths()
    target = artifacts['web']
    if (not force) and target.exists():
        print(f"Target {target} already exists")
        return
    with ctx.cd('web'):
        print("Cleaning up directory")
        ctx.run('rm -rf dist')
        ctx.run('mkdir dist')
        print("Building vue package")
        ctx.run('npm ci')
        ctx.run('npm run build')
    ctx.run('mkdir -p ./dist')
    ctx.run(f"tar -C ./web/dist/ -czf {target} .")
    if github_output:
        print_github_kv('ASSET_NAME', target.name)
        print_github_kv('PATH', target)

ns = Collection('web')
ns.add_task(build)
