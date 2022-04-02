![Snoozeweb Logo](https://github.com/snoozeweb/snooze/raw/master/web/public/img/logo.png)

# About

Snooze is a powerful monitoring tool used for log aggregation and alerting. It comes with the following features:
* Backend + Web interface
* Local / LDAP / JWT token based authentication
* Built-in clustering for scalability
* Large number of sources as inputs
* Log aggregation
* Log manipulation
* Log archiving
* Alerting policies
* Various alerting methods
* Auto housekeeping
* Auto backups
* Metrics

Try it now on: https://try.snoozeweb.net

![Alerts](https://github.com/snoozeweb/snooze/raw/master/doc/images/web_alerts.png)

# Installation

Installation on CentOS/RHEL

```bash
$ wget https://rpm.snoozeweb.net -O snooze-server-latest.rpm
$ sudo yum localinstall snooze-server-latest.rpm
$ sudo systemctl start snooze-server
```

Installation on Ubuntu/Debian

```bash
$ wget https://deb.snoozeweb.net -O snooze-server-latest.deb
$ sudo apt install snooze-server-latest.deb
$ sudo systemctl start snooze-server
```

Web interface URL:

```
http://localhost:5200
```

if `create_root_user` in `/etc/snooze/core.yaml` has not been set to **false**, login credentials are `root:root`

Otherwise, it is always possible to generate a root token that can be used for **JWT Token** authentication method if [Snooze Client](https://github.com/snoozeweb/snooze_client) is installed:

```bash
$ snooze root-token
# Run with root or snooze user
Root token: eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.eyJ1c2VyIjp7Im...
```

## Recommendations

By default, Snooze is using a single file to store its database and therefore can run out of the box without any additional configuration or dependency. While this implementation is convenient for testing purpose, it is heavily recommended to switch the database configuration to MongoDB.

## Docker

### Simple

```
$ docker run --name snoozeweb -d -p <port>:5200 snoozeweb/snooze
```

Then the Web interface should be available at this URL:

```
http://<docker>:<port>
```

Snoozeweb docker image can be run without any backend database (will default to a file based DB) but if one is needed:

```
$ docker run --name snooze-db -d mongo
```

Then

```
$ export DATABASE_URL=mongodb://db:27017/snooze
$ docker run --name snoozeweb -e DATABASE_URL=$DATABASE_URL --link snooze-db:db \
-d -p <port>:5200 snoozeweb/snooze
```

### Advanced

Requirements:
* Docker (version >= 17.0.0)
* Docker-Compose

The `docker-compose.yaml` recipe will create the following (Replace `HOST1`, `HOST2` and `HOST3` with swarm workers):
* 3 nodes MongoDB Replica Set
* 3 Snooze servers in cluster mode
* 1 Nginx load balancer (manager node)

After initializing [docker swarm](https://docs.docker.com/engine/swarm/) and adding workers, run the command:
```
docker stack deploy -c docker-compose.yaml snoozeweb
# Wait until MongoDB containers are up
replicate="rs.initiate(); sleep(1000); cfg = rs.conf(); cfg.members[0].host = \"mongo1:27017\"; rs.reconfig(cfg); rs.add({ host: \"mongo2:27017\", priority: 0.5 }); rs.add({ host: \"mongo3:27017\", priority: 0.5 }); rs.status();"
docker exec -it $(docker ps -qf label=com.docker.swarm.service.name=snoozeweb_mongo1) /bin/bash -c "echo '${replicate}' | mongo"
```

The web interface should be available on the manager node on port 80

# Configuration

The only configuration file not managed in the web interface is `/etc/snooze/core.yaml` and requires restarting Snooze if changed.

`/etc/snooze/core.yaml`
* `listen_addr` (`'0.0.0.0'`): IPv4 address on which Snooze process is listening to
* `port` (`5200`): Port on which Snooze process is listening to
* `debug` (`false`): Activate debug log output
* `bootstrap_db` (`true`): Populate the database with an initial configuration
* `create_root_user` (`true`): Create a *root* user with a default password *root*
* `no_login` (`false`): Disable Authentication (everyone has admin priviledges)
* `audit_excluded_paths` (`[/api/patlite', /metrics, /web]`): List of HTTP paths excluded from audit logs
* `ssl`
    * `enabled` (`false`): Enable TLS termination for both the API and the web interface
    * `certfile` (`''`): Path to the SSL certificate
    * `keyfile` (`''`): Path to the private key
* `web`
    * `enabled` (`true`): Enable the web interface
    * `path` (`/opt/snooze/web`): Path to the web interface dist files
* `clustering`
    *  `enabled` (`false`): Enable clustering mode
    * `members`: List of snooze servers in the cluster {host, port}
        - `host` (`localhost`): Hostname or IPv4 address of the first member

          `port` (`5200`): Port on which the first member is listening to
        - ...
* `database`
    * `type` (`file`): Backend database to use (file or mongo)
* `backup`
    * `enabled` (`true`): Enable backups
    * `path` (`WORKDIR/backups`): Path to store database backups
    * `exclude` ([record, stats, comment, secrets]): Collections to exclude from backups

Example for MongoDB backend with database replication enabled:
```yaml
database:
    type: mongo
    host:
        - hostA
        - hostB
        - hostC
    port: 27017
    username: snooze
    password: 7dg9khqg1w6
    authSource: snooze
    replicaSet: rs0
```

# Documentation

[Documentation page](doc/)

# License

```
Snooze - Log aggregation and alerting

Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.
```
