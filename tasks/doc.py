'''A series of tasks to generate documentation'''

import difflib
from pathlib import Path

import yaml
from invoke import task, Collection
from jinja2 import Environment, PackageLoader

from snooze import __file__ as rootdir

#from tasks.utils import get_versions, print_github_kv, get_paths
PROJECT_PATH = Path(rootdir).parent.parent

def rst_title(text, char, double=False):
    '''Generate a RST style title:
    CORE
    ===='''
    title = len(text) * char
    if double:
        return f"{title}\n{text}\n{title}"
    else:
        return f"{text}\n{title}"

def compute_type(prop: dict) -> str:
    if '$ref' in prop:
        ref_name = prop['$ref'].split('/')[-1]
        return f":ref:`{ref_name}<{ref_name}>`"
    if 'enum' in prop:
        return ' | '.join(map(repr, prop['enum']))
    if 'anyOf' in prop:
        return ' | '.join([compute_type(obj) for obj in prop['anyOf']])
    if 'allOf' in prop:
        return ' + '.join([compute_type(obj) for obj in prop['allOf']])
    if prop['type'] == 'array' and 'items' in prop:
        item_type = compute_type(prop['items'])
        return f"array[{item_type}]"
    if 'type' in prop:
        text = prop['type']
        if 'format' in prop:
            text += f" ({prop['format']})"
        return text
    return 'unknown'

def append_dot(line: str) -> str:
    if line[-1] != '.':
        line += '.'
    return line

def rst_prop(name: str, prop: dict, required: bool, title_level='=') -> str:
    text = ''
    # Title
    text += rst_title(name, title_level) + "\n\n"
    # Table
    prop_type = compute_type(prop)
    if prop_type:
        text += f"    :Type: {prop_type}\n"
    if required:
        text += '    :Required: True'
    if 'env' in prop:
        text += f"    :Environment variable: ``{prop['env']}``\n"
    if 'default' in prop:
        text += f"    :Default: ``{repr(prop['default'])}``\n"
    text += "\n"

    # Description
    if 'title' in prop and 'description' not in prop:
        description = prop['title']
    if 'description' in prop:
        description = prop['description']
    else:
        description = ''
    if description:
        text += "\n".join(['    ' + line for line in description.splitlines()]) + "\n\n"
    if 'examples' in prop:
        for index, example in enumerate(prop['examples']):
            text += f"    .. admonition:: Example {index+1}\n\n"
            text += '        .. code-block:: yaml\n\n'
            text += "\n".join([' '*12 + line for line in yaml.dump({name: example}).splitlines()])
            text += "\n\n"
    return text

def rst_definition(name: str, definition: dict) -> str:
    text = ''
    # Reference
    text += f".. _{name}:\n\n"
    # Title
    if 'title' in definition and definition['title'] != name:
        title = definition['title']
    else:
        title = name
    text += rst_title(title, '=') + "\n\n"
    # Description
    if 'description' in definition:
        text += f"{definition['description']}\n\n"
    if 'properties' in definition:
        for name, prop in definition['properties'].items():
            required = (name in definition.get('required', []))
            text += rst_prop(name, prop, required, '-') + "\n\n"
    return text

def rst_schema(schema: dict) -> str:
    text = ''
    # Title
    text += rst_title(schema['title'], '#', double=True) + "\n\n"
    # Description
    text += f"    :Package location: ``{schema['path']}``\n"
    text += f"    :Live reload: ``{schema['live']}``\n"
    text += "\n"
    if 'description' in schema:
        text += f"{schema['description']}\n\n"
    # Properties
    if 'properties' in schema:
        text += rst_title('Properties', '*', double=True) + "\n\n"
        for name, prop in schema['properties'].items():
            required = (name in schema.get('required', []))
            text += rst_prop(name, prop, required) + "\n\n"
        text += "\n"
    # Definitions
    if 'definitions' in schema:
        text += rst_title('Definitions', '*', double=True) + "\n\n"
        for name, definition in schema['definitions'].items():
            text += rst_definition(name, definition) + "\n\n"
        text += "\n\n"
    return text

@task
def config(ctx):
    '''Generate documentation for configuration files'''
    from snooze.utils.config import CoreConfig, GeneralConfig, HousekeeperConfig, NotificationConfig, LdapConfig
    configs = [CoreConfig, GeneralConfig, HousekeeperConfig, NotificationConfig, LdapConfig]
    for config in configs:
        text = ''
        text += rst_schema(config.schema())
        path = PROJECT_PATH / f"docs/configuration/{config.__config__.section}.rst"
        try:
            before = path.read_text(encoding='utf-8')
        except:
            before = ''
        diff = "\n".join(difflib.unified_diff(before.splitlines(), text.splitlines()))
        print(diff)
        path.write_text(text, encoding='utf-8')
        print(f"Documentation generated in {path}")

@task(config)
def sphinx(ctx):
    '''Build HTML documentation from RST using Sphinx'''
    with ctx.cd('docs'):
        ctx.run('make html')

ns = Collection('doc')
ns.add_task(config)
ns.add_task(sphinx)
