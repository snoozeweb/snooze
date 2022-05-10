'''Build the rpm'''

from pathlib import Path
from tempfile import TemporaryDirectory

from invoke import task, Collection

import tasks.pip
import tasks.web
from tasks.utils import print_github_kv, get_paths, get_versions
from snooze.utils.config import *

def compute_type(prop: dict) -> str:
    if prop['type'] == 'array' and prop.get('items', {}).get('type'):
        return f"array[{prop['items']['type']}]"
    else:
        return prop['type']

def get_ref(obj: dict) -> str:
    if '$ref' in obj:
        return obj['$ref']
    if 'allOf' in obj:
        return obj['allOf'][0]['$ref']
    return None

def indent(text: str, level: int = 0) -> str:
    ind = '  ' * level
    return "\n".join([
        f"{ind}{line}" if line != '' else line for line in text.splitlines()
    ])

def comment(text: str) -> str:
    '''Return a commented multi-line'''
    return "\n".join([f"# {line}" for line in text.splitlines()]) + "\n"

def prop_to_yaml(schema: dict, name: str, prop: dict, required: bool) -> str:
    output = ''
    title_line = ''
    if 'title' in prop:
        title_line += f"{prop['title']}"
    if 'type' in prop:
        title_line += f" ({compute_type(prop)})"
    output += comment(title_line)
    ref = get_ref(prop)
    if 'description' in prop:
        output += comment(f"{prop['description']}")
    if 'examples' in prop:
        for index, example in enumerate(prop['examples']):
            output += comment(f"Example #{index}:")
            output += comment(yaml.safe_dump({name: example}))
    if ref:
        output += f"{name}:"
        ref_name = ref.split('/')[-1]
        definition = schema['definitions'][ref_name]
        output += indent(schema_to_yaml(definition), 1)
    elif 'default' in prop:
        output += yaml.safe_dump({name: prop['default']})
    elif 'examples' in prop:
        pass
    else:
        output += comment(f"{name}: ")
    return output

def schema_to_yaml(schema: dict) -> str:
    '''Transform a JSON schema to a yaml default file'''
    output = ''
    if 'title' in schema:
        output += comment(f"{schema['title']}")
    if 'description' in schema:
        output += comment(f"{schema['description']}")
    output += "\n"
    if 'properties' in schema:
        for name, prop in schema['properties'].items():
            required = (name in schema.get('required', []))
            output += f"{prop_to_yaml(schema, name, prop, required)}\n"
    #if 'definitions' in schema:
    #    output += "## Definitions\n\n"
    #    for name, definition in schema['definitions'].items():
    #        output += f"{definition_to_markdown(name, definition)}\n"
    #    output += "\n"
    return output

@task
def defaults(ctx):
    basedir = Path('rpm')
    configs = [CoreConfig, GeneralConfig, HousekeeperConfig, NotificationConfig, LdapConfig]
    for config in configs:
        config_path = basedir / f"{config._section}.yaml"
        data = "---\n"
        data += schema_to_yaml(config.schema())
        config_path.write_text(data, encoding='utf-8')
        print(f"Default file updated at {config_path}")


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
        ctx.run(f"cp packaging/files/* {tmpdir}/SOURCES/")
        ctx.run(f"rpmbuild --define '_topdir {tmpdir}' --ba packaging/snooze-server.spec -vv --debug")
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
ns.add_task(defaults)
