import click

from snooze_client.process import Snooze

COMMON_OPTIONS = [
    click.option('--server', '-s', 'URI of the Snooze server'),
]

def add_options(options):
    def callback(func):
        for option in reversed(options):
            func = option(func)
        return func
    return callback

@click.group()
def snooze():
    pass

@snooze.command()
@add_options(COMMON_OPTIONS)
@click.argument('keyvalues')
def alert(server, keyvalues):
    client = Snooze(server)
    record = {}
    for keyvalue in keyvalues:
        if '=' not in keyvalue:
            raise ValueError("Options must be of format key=value")
        key, value = keyvalue.split('=')
        record[key] = value
    client.alert(record)
