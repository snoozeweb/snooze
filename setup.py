#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

from setuptools import setup, find_packages

with open("README.md", "r") as f:
    long_description = f.read()
with open("VERSION", "r") as f:
    version = f.read().rstrip('\n')

setup(
    name="snooze-server",
    version=version,
    author="Florian Dematraz, Guillaume Ludinard",
    description="Monitoring tool for logs aggregation and alerting",
    long_description=long_description,
    long_description_content_type="text/markdown",
    packages=find_packages(exclude=("tests",)),
    classifiers=[
        'License :: OSI Approved :: GNU Affero General Public License v3 or later (AGPLv3+)',
    ],
    package_data={
        'snooze': [
            'defaults/*.yaml',
            'plugins/core/*/metadata.yaml',
            'plugins/action/*/metadata.yaml',
        ],
    },
    include_package_data=True,
    entry_points={
        'console_scripts': [
            'snooze-server = snooze.__main__:main',
            'snooze = snooze.cli.__main__:snooze',
        ],
    },
    install_requires = [
        'Jinja2',
        'PyJWT==1.7.1',
        'PyYAML==5.4.1',
        'click',
        'falcon',
        'falcon-auth',
        'ldap3',
        'pathlib',
        'prometheus-client',
        'pymongo',
        'python-dateutil',
        'requests_unixsocket',
        'tinydb',
        'urllib3',
        'netifaces',
        'pyparsing',
    ],
)
