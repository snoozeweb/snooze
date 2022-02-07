# Contributing

## Setup

Install poetry
```bash
pip3 install 'poetry>=1.2.0a2' --user
```

Clone this git:
```bash
git clone https://github.com/snoozeweb/snooze
cd snooze
```

Install dependencies:
```bash
poetry install
```

## Running unit tests

```bash
poetry run pytest
```

## Running the server locally

At the moment, we're using a specific default logging configuration, so you either need to:
* Create `/var/log/snooze` directory
* Or edit `snooze/defaults/logging.yaml` to remove the `file` configuration

THen you can run the server in the following manner:
```bash
poetry run snooze-server
```

The server will bind to `*:5200`.

# Building

```bash
poetry build
```

The builds will be placed in the `dist/` directory. If you're building versions, you will need to
manage the version in `pyproject.toml`.

## Web interface

[CONTRIBUTE.md](web/CONTRIBUTE.md)
