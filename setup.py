#!/usr/bin/python3.6

from setuptools import setup, find_packages

with open("README.md", "r") as f:
    long_description = f.read()

setup(
    name="snooze",
    version="0.0.1",
    author="Florian Dematraz, Guillaume Ludinard",
    description="Yet another alerting system",
    long_description=long_description,
    long_description_content_type="text/markdown",
    packages=find_packages(),
    entry_points={
        'console_scripts': [
            'snooze-client = client.cli:snooze',
            'snooze-syslog = snooze.plugins.input.syslog:main',
            'snooze-server = snooze.__main__:main',
            'snooze = snooze.cli.__main__:snooze',
        ],
    },
    classifiers=[],
)
