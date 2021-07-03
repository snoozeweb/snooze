#!/usr/bin/python36
import yaml
from os.path import dirname
from os.path import join as joindir
from snooze import __file__ as rootdir
from snooze.utils import config, write_config

class Action:
    def __init__(self, core):
        self.core = core
        self.name = self.__class__.__name__.lower()
        metadata_path = joindir(dirname(rootdir), 'plugins', 'action', self.name, 'metadata.yaml')
        self.metadata_file = {}
        try:
            with open(metadata_path, 'r') as f:
                self.metadata_file = yaml.load(f.read())
        except:
            pass
        self.metadata_file['action_name'] = self.name

    def get_metadata(self):
        return self.metadata_file

    def get_icon(self):
        return self.metadata_file.get('icon', 'question-circle')

    def pprint(self, content):
        return self.name

    def send(self, record, content):
        pass
