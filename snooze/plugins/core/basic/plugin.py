import yaml
from os.path import dirname
from os.path import join as joindir
from snooze import __file__ as rootdir
from logging import getLogger
log = getLogger('snooze')

class Plugin:
    def __init__(self, core):
        self.core = core
        self.db = core.db
        self.name = self.__class__.__name__.lower()
        metadata_path = joindir(dirname(rootdir), 'plugins', 'core', self.name, 'metadata.yaml')
        self.metadata_file = {}
        try:
            log.debug("Attempting to read metadata at %s for %s module", metadata_path, self.name)
            with open(metadata_path, 'r') as f:
                self.metadata_file = yaml.load(f.read())
        except Exception as err:
            log.debug("Skipping. Cannot read metadata.yaml due to: %s", err)
        self.permissions = self.metadata_file.get('provides', [])
        default_routeclass = self.metadata_file.get('class', 'Route')
        default_authorization = self.metadata_file.get('authorization_policy')
        default_duplicate = self.metadata_file.get('duplicate_policy', 'update')
        default_checkpermissions = self.metadata_file.get('check_permissions', False)
        default_checkconstant = self.metadata_file.get('check_constant')
        default_injectpayload = self.metadata_file.get('inject_payload', False)
        default_prefix = self.metadata_file.get('prefix', '/api')
        if self.metadata_file.get('action_form'):
            self.metadata_file['action_name'] = self.name
        if default_routeclass:
            routes = {
                '/'+self.name: {
                    'class': default_routeclass,
                    'authorization_policy': default_authorization,
                    'duplicate_policy': default_duplicate,
                    'check_permissions': default_checkpermissions,
                    'check_constant': default_checkconstant,
                    'inject_payload': default_injectpayload,
                    'prefix': default_prefix
                },
                '/'+self.name+'/{search}': {
                    'class': default_routeclass,
                    'authorization_policy': default_authorization,
                    'duplicate_policy': default_duplicate,
                    'check_permissions': default_checkpermissions,
                    'check_constant': default_checkconstant,
                    'inject_payload': default_injectpayload,
                    'prefix': default_prefix
                },
                '/'+self.name+'/{search}/{nb_per_page}/{page_number}': {
                    'class': default_routeclass,
                    'authorization_policy': default_authorization,
                    'duplicate_policy': default_duplicate,
                    'check_permissions': default_checkpermissions,
                    'check_constant': default_checkconstant,
                    'inject_payload': default_injectpayload,
                    'prefix': default_prefix
                },
                '/'+self.name+'/{search}/{nb_per_page}/{page_number}/{order_by}/{asc}': {
                    'class': default_routeclass,
                    'authorization_policy': default_authorization,
                    'duplicate_policy': default_duplicate,
                    'check_permissions': default_checkpermissions,
                    'check_constant': default_checkconstant,
                    'inject_payload': default_injectpayload,
                    'prefix': default_prefix
                }
            }
            if 'routes' in self.metadata_file:
                routes.update(self.metadata_file['routes'])
            self.metadata = {
                'name': self.name,
                'auto_reload': self.metadata_file.get('auto_reload', True),
                'default_sorting': self.metadata_file.get('default_sorting', ''),
                'default_ordering': self.metadata_file.get('default_ordering', True),
                'primary': self.metadata_file.get('primary', None),
                'routes': routes
            }
        else:
            self.metadata = self.metadata_file

    def post_init(self):
        self.reload_data()

    def reload_data(self, sync = False):
        if self.metadata.get('auto_reload', True):
            log.debug("Reloading data for plugin {}".format(self.name))
            self.data = self.db.search(self.name, orderby=self.metadata.get('default_sorting', ''), asc=self.metadata.get('default_ordering', True))['data']

    def process(self, record):
        return record

    def get_metadata(self):
        return self.metadata

    def pprint(self):
        return self.name

    def get_icon(self):
        return self.metadata_file.get('icon', 'question-circle')

    def send(self, record, content):
        pass

class Abort(Exception): pass
class Abort_and_write(Exception): pass
