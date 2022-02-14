'''Manage the pip/python operations'''

from pathlib import Path

from invoke import task, Collection

from tasks.utils import get_version, print_github_kv

@task
def build(ctx, force=False, github_output=False):
    '''Package the python code into a pip package (output in dist/)'''
    ver, rel = get_version()
    ver_rel = f"{ver}+{rel}" if rel else ver
    target = Path(f"dist/snooze_server-{ver_rel}-py3-none-any.whl")
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
