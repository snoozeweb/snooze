'''A series of tasks to generate documentation'''

from pathlib import Path

from invoke import task, Collection
from jinja2 import Environment, PackageLoader

from snooze.utils.config import *

#from tasks.utils import get_versions, print_github_kv, get_paths

def compute_type(prop: dict) -> str:
    if '$ref' in prop:
        ref_name = prop['$ref'].split('/')[-1]
        return f"[{ref_name}](#{ref_name})"
    if 'enum' in prop:
        return '|'.join(map(repr, prop['enum']))
    if 'anyOf' in prop:
        return '|'.join([compute_type(obj) for obj in prop['anyOf']])
    if 'allOf' in prop:
        return '+'.join([compute_type(obj) for obj in prop['allOf']])
    if prop['type'] == 'array' and prop.get('items', {}).get('type'):
        return f"array[{prop['items']['type']}]"
    if 'type' in prop:
        return prop['type']
    return 'unknown'

def append_dot(line: str) -> str:
    if line[-1] != '.':
        line += '.'
    return line

def prop_to_markdown(name: str, prop: dict, required: bool) -> str:
    prop_line = '- '
    prop_line += f"`{name}`"
    prop_type = compute_type(prop)
    if prop_type:
        prop_line += f" ({prop_type})"
    if required:
        prop_line += ' (**required**)'
    prop_line += ':' # The 2 spaces matters for indentation
    if 'title' in prop and 'description' not in prop:
        prop_line += f" {append_dot(prop['title'])}  "
    if 'description' in prop:
        prop_line += f" {append_dot(prop['description'])}  "
    if 'env' in prop:
        prop_line += f"\n**Environment variable**: `{prop['env']}`.  "
    if 'default' in prop:
        prop_line += f"\n**Default**: `{prop['default']}`.  "
    if 'examples' in prop:
        prop_line += f"\n**Example(s)**:  \n"
        for example in prop['examples']:
            prop_line += f"    - `{example}`\n"
    return prop_line

def definition_to_markdown(name: str, definition: dict) -> str:
    output = ''
    if 'title' in definition and definition['title'] != name:
        output += f"### {definition['title']} ({name})\n\n"
    else:
        output += f"### {name}\n\n"
    if 'description' in definition:
        output += f"{definition['description']}\n\n"
    if 'properties' in definition:
        output += "#### Properties\n\n"
        for name, prop in definition['properties'].items():
            required = (name in definition.get('required', []))
            output += f"{prop_to_markdown(name, prop, required)}\n"
    return output

def schema_to_markdown(schema: dict) -> str:
    '''Convert the schema to markdown'''
    output = ''
    if 'title' in schema:
        output += f"# {schema['title']}\n\n"
    if 'description' in schema:
        output += f"{schema['description']}\n\n"
    if 'properties' in schema:
        output += "## Properties\n\n"
        for name, prop in schema['properties'].items():
            required = (name in schema.get('required', []))
            output += f"{prop_to_markdown(name, prop, required)}\n"
        output += "\n"
    if 'definitions' in schema:
        output += "## Definitions\n\n"
        for name, definition in schema['definitions'].items():
            output += f"{definition_to_markdown(name, definition)}\n"
        output += "\n"
    return output

@task
def config(ctx):
    '''Generate documentation for configuration files'''
    doc_path = Path('doc/15_Configuration.md')
    configs = [CoreConfig, GeneralConfig, HousekeeperConfig, NotificationConfig, LdapConfig]
    print(CoreConfig.schema_json(indent=2))
    doc_str = "# Summary\n\n"
    for config in configs:
        title = config.schema()['title']
        doc_str += f"* [{title}](#{title.replace(' ', '-').lower()})\n"
    doc_str += "\n"
    for config in configs:
        doc_str += schema_to_markdown(config.schema())
    doc_path.write_text(doc_str, encoding='utf-8')
    print(f"Documentation generated in {doc_path}")

ns = Collection('doc')
ns.add_task(config)
