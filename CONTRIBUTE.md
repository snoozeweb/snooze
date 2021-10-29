# Contributing

## Setup

Install [pyenv](https://github.com/pyenv/pyenv) and [pyenv-virtualenv](https://github.com/pyenv/pyenv-virtualenv)
by following the instructions.

Clone this git:
```bash
git clone https://github.com/snoozeweb/snooze
cd snooze
```

Install `pipenv`:
```bash
pip install pipenv
```

Install the version of python, as well as the package dependencies:
```bash
pipenv install --dev
```

Install snooze-server
```bash
pipenv install -e .
```

Create a directory for the snooze server socket:
```bash
sudo mkdir /var/run/snooze
sudo chmod 777 /var/run/snooze
```

Note: The socket path can be overwritten with the config file `/etc/snooze/server/core.yaml`
```
unix_socket: /var/run/snooze/server.socket
```

The config file path itself can be changed with the environment variable `SNOOZE_CONFIG_PATH`
```bash
export SNOOZE_SERVER_CONFIG=/etc/snooze/server
```

## Web interface

[CONTRIBUTE.md](web/CONTRIBUTE.md)
