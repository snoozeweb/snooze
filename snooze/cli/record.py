import click
import requests
import json

from snooze.cli.login import get_token

@click.group()
def record():
    pass

@record.command()
@click.option('-d', '--data', help='JSON data of the record (inline)')
def post(data):
    token = get_token()
    headers = {'Authorization': 'JWT {}'.format(token), 'Content-Type': 'application/json'}
    json_data = json.loads(data)
    response = requests.post('http://localhost:5200/api/record', headers=headers, data = json_data)
    print(response)

@record.command()
def list():
    token = get_token()
    headers = {'Authorization': 'JWT {}'.format(token)}
    response = requests.get('http://localhost:5200/api/record', headers=headers)
    json = response.json()
    data = json.get('data')
    if data:
        for record in data:
            print(record)
    else:
        print('No data')
