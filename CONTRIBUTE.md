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

For python unit tests, run the following:
```bash
poetry run pytest
```

## Running the server locally

At the moment, we're using a specific default logging configuration, so you either need to:
* Create `/var/log/snooze` directory
* Or edit `snooze/defaults/logging.yaml` to remove the `file` configuration

Then you can run the server in the following manner:
```bash
poetry run snooze-server
```

The server will bind to `*:5200`.

## Running the web interface locally

When running the web server in development mode, the name of the backend need to be specified
in `web/.env.development.local`.

Example of a local server configuration:
```javascript
# .env.development.local
VUE_APP_API = "http://10.0.0.10:5200/api"
```

The dependencies can be downloaded as such:
```bash
cd ./web
npm ci
```
Note that it requires a recent version of nodejs (see `web/package.json` for the exact requirements).

The development web server can then be started as such:
```bash
npm run serve
```

# Development builds

Versions of the build packages are managed by `pyproject.toml`.
In order to get automatic versionning during the dev process, it is recommended:
* To commit every change before building (to take advantage of commit hash for identification).
* To use the `poetry run invoke dev-build` job (whihc build every packages).

Builds are by default lazy: If a package of a given version is already built, the build will not
be triggered again. This can be changed by the `--force` argument in most build jobs, or in `./invoke.yaml` (see
invoke documention).

## Python package

```bash
poetry run invoke pip.build
```

The builds will be placed in the `dist/` directory. If you're building versions, you will need to
manage the version in `pyproject.toml`.

## Web interface

More details:
[CONTRIBUTE.md](web/CONTRIBUTE.md)

There is also a job to build it:
```bash
poetry run invoke web.build
```

The job requires a recent version of node (see `web/package.json` for the exact requirements).
The build result will be placed in `dist/`

## RPM

The rpm build rely on the `web.build` and `pip.build`. It requires the `rpmbuild` binary.
```bash
poetry run invoke rpm.build
```
The build result will be placed in `dist/`

## Docker image

The docker build rely on `web.build` and `pip.build`. It requires a recent version of docker installed, as
well as the current user being in the docker group.
```bash
poetry run invoke docker.build
```
The build result can be listed in docker afterwards:
```bash
docker images
```
