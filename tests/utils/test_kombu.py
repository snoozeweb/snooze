'''Test for the Message Queue thread'''

from time import sleep

from kombu import Connection, Exchange, Queue, Consumer
from kombu.mixins import ConsumerMixin

from snooze.utils.kombu import MongodbTransport

class TestMongodbTransport:

    data = {
        'messages.broadcast': [
            # We need to initialize the collection with something to avoid having
            # kombu initializing the collection. Because kombu passes kwargs to
            # mongomock's create_collection, and mongomock doesn't support it.
            {'name': 'test1', 'content': 'dummy message'},
        ],
    }

    def test_transport(self, db):
        mongo_client = db.db
        exchange = Exchange('tasks', type='direct')

        def handle_message(body, _message):
            assert body['name'] == 'test2'
            assert body['content'] == 'message 2'

        queue = Queue('myqueue', exchange, routing_key='myqueue')
        connection = Connection(transport=MongodbTransport,
            transport_options={'database': mongo_client, 'confirm_publish': True})
        try:
            consumer = Consumer(connection, queues=[queue], accept=['json'], callbacks=[handle_message])
            producer = connection.Producer(serializer='json')
            payload = {'name': 'test2', 'content': 'message 2'}
            producer.publish(payload, exchange=exchange, declare=[exchange], routing_key='myqueue')
            consumer.consume()
        finally:
            connection.drain_events()
            connection.close()
