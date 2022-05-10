.. _docker:

=================
Docker deployment
=================

Simple
======

.. code-block:: console

    $ docker run --name snoozeweb -d -p <port>:5200 snoozeweb/snooze

Then the Web interface should be available at this URL:

.. code-block:: console

    http://<docker>:<port>

Snoozeweb docker image can be run without any backend database (will default to a file based DB) but if one is needed:

.. code-block:: console

    $ docker run --name snooze-db -d mongo

Then

.. code-block:: console

    $ export DATABASE_URL=mongodb://db:27017/snooze
    $ docker run --name snoozeweb -e DATABASE_URL=$DATABASE_URL \
        --link snooze-db:db -d -p <port>:5200 snoozeweb/snooze

The web interface will listen locally on port 5200: http://localhost:5200

Advanced
========

Requirements:

* Docker (version >= 17.0.0)
* Docker-Compose

The `docker-compose.yaml <https://github.com/snoozeweb/snooze/blob/master/docker-compose.yaml>`_ recipe will create the following (Replace ``HOST1``, ``HOST2`` and ``HOST3`` with swarm workers):

* 3 nodes MongoDB Replica Set
* 3 Snooze servers in cluster mode
* 1 Nginx load balancer (manager node)

After initializing `docker swarm <https://docs.docker.com/engine/swarm/>`_ and adding workers, run the command:

.. code-block:: console

    $ docker stack deploy -c docker-compose.yaml snoozeweb
    # Wait until MongoDB containers are up
    $ replicate="rs.initiate(); sleep(1000); cfg = rs.conf(); cfg.members[0].host = \"mongo1:27017\"; rs.reconfig(cfg); rs.add({ host: \"mongo2:27017\", priority: 0.5 }); rs.add({ host: \"mongo3:27017\", priority: 0.5 }); rs.status();"
    $ docker exec -it $(docker ps -qf label=com.docker.swarm.service.name=snoozeweb_mongo1) /bin/bash -c "echo '${replicate}' | mongo"

The web interface will be available on the manager node on port 80
