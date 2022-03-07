'''Manage the pip/python operations'''

from invoke import task, Collection

from tasks.utils import print_github_kv, get_paths

@task
def build(ctx, force=False, github_output=False):
    '''Package the python code into a pip package (output in dist/)'''
    artifacts = get_paths()
    target = artifacts['wheel']
    if (not force) and target.exists():
        print(f"Target {target} already exists")
        return
    print("Building python pip package")
    ctx.run("poetry build")
    if github_output:
        print_github_kv('ASSET_NAME', target.name)
        print_github_kv('PATH', target)

ns = Collection('pip')
ns.add_task(build)
