# Contributing

## Running node virtual environment

You might want to install the specific version of node used by this project.
`n` or `nvm` projects can make it easier to manage.

Here is the method with `nvm`:
* Install the project [here](https://github.com/nvm-sh/nvm)
* Log in again with a new sesion (or source your `.bashrc`/`.bash_profile`)
* In this project directory, run `nvm install $(cat .node-version)`
* Verify with `nvm list` or `node --version`

Automatic change of directory can be provided by `avn`:
* `npm install -g avn avn-nvm`
* `avn setup`

Once you change into the project directory:
```
$cd snooze-web/
avn activated 8.16.1

$node --version
v8.16.1
```

Make sure to install the dependencies for the project:
```bash
npm install
```

## Running the dev environment

Install npm on your system.

You need to run an instance of `snooze-server`.
```bash
pip install snooze
snooze-server &
```

Get a root token:
```bash
snooze root-token
```

Prepare the `.env.development.local` as follow:
```javascript
VUE_APP_API = "http://localhost:9000"
```

Please note that you need to change the localhost part if you're running
snooze-server on a remote machine, even if snooze-web is installed on the
same machine. It's the browser making the request to the snooze-server API
after all.

Run the snooze-web dev environment:

```
npm run serve
```

You can now change the files, and they will be updated dynamically.

## Running unit tests

```bash
npm run test:unit
```
