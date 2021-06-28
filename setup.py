#!/usr/bin/python3.6

from setuptools import setup, find_packages

with open("README.md", "r") as f:
    long_description = f.read()

setup(
    name="snooze-server",
    version="0.0.9",
    author="Florian Dematraz, Guillaume Ludinard",
    description="Yet another alerting system",
    long_description=long_description,
    long_description_content_type="text/markdown",
    packages=find_packages(),
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
        'PyJWT',
        'PyYAML',
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
    ],
)
