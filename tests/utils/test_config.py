from pathlib import Path

from snooze.utils.config import config, write_config

def test_config():
    default_config = config()
    assert default_config['core_plugin'] == ['record']

def test_write_config(tmp_path):
    write_config('test_config', {'a': 1, 'b': 2}, tmp_path)

    yaml_content = "a: 1\nb: 2\n"

    tmp_file = Path(tmp_path) / 'test_config.yaml'
    assert tmp_file.is_file()
    assert tmp_file.read_text() == yaml_content
