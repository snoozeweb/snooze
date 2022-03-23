import time
import sys
import threading
import bson.json_util
from random import random
from itertools import count
from logging import getLogger
from kombu import Exchange, Queue
from kombu.mixins import ConsumerMixin
from kombu.pools import producers
from kombu.serialization import register
from kombu import Connection
log = getLogger('snooze.mq')

task_exchange = Exchange('tasks', type='direct')

class MQManager():
    def __init__(self, core):
        log.debug('Init MQManager')
        self.core = core
        self.url = core.db.get_uri()
        self.threads = {}
        if not self.url:
            self.url = 'memory:///'
        register('bson', bson.json_util.dumps, bson.json_util.loads,
                 content_type='application/json',
                 content_encoding='utf-8')

    def update_queue(self, queue, timer = 10, maxsize = 100, worker_class = None, worker_obj = None):
        if queue not in self.threads:
            self.threads[queue] = MQThread(self, queue, timer, maxsize, worker_class, worker_obj)
            self.threads[queue].start()
        else:
            self.threads[queue].update(timer, maxsize, worker_obj)
        return self.threads[queue]

    def remove_queue(self, queue):
        if queue in self.threads:
            self.threads[queue].worker.end = True
            return self.threads[queue]
        return None

    def keep_queues(self, queues, prefix = ''):
        for queue, thread in self.threads.items():
            if queue.startswith(prefix) and queue not in queues:
                log.debug("Trying to clean queue {}".format(queue))
                thread.worker.end = True

    def send(self, queue, payload):
        connection = self.threads[queue].connection
        if connection:
            try:
                with producers[connection].acquire(block=True) as producer:
                    producer.publish(payload,
                                     serializer='bson',
                                     exchange=task_exchange,
                                     declare=[task_exchange],
                                     routing_key=queue)
                return True
            except Exception as e:
                log.exception(e)
                return False
        else:
            log.error("Queue `{}` is disconnected. Cannot send message".format(queue))
            return False

class MQThread(threading.Thread):

    def __init__(self, manager, queue, timer = 10, maxsize = 100, worker_class = None, worker_obj = None):
        super().__init__()
        self.manager = manager
        self.queue = Queue(queue, task_exchange, routing_key=queue)
        if worker_class:
            self.worker_class = worker_class
        else:
            self.worker_class = Worker
        self.main = threading.main_thread()
        self.timer = timer
        self.maxsize = maxsize
        self.obj = worker_obj
        self.worker = None

    def update(self, timer = 10, maxsize = 100, worker_obj = None):
        self.timer = timer
        self.maxsize = maxsize
        self.obj = worker_obj

    def run(self):
        with Connection(self.manager.url) as conn:
            self.connection = conn
            self.worker = self.worker_class(conn, self)
            self.worker.run()

class Worker(ConsumerMixin):

    def __init__(self, connection, thread):
        self.connection = connection
        self.thread = thread
        self.to_ack = []
        self.wait_time = 0
        self.end = False
        self.can_process = False

    def get_consumers(self, Consumer, channel):
        self.consumer = Consumer(queues=[self.thread.queue], accept=['json'], callbacks=[self.add_msg])
        return [self.consumer]

    def add_msg(self, body, message):
        if (body, message) not in self.to_ack:
            self.to_ack.append((body, message))
        self.try_process()

    def try_process(self):
        if self.can_process and len(self.to_ack) > 0:
            if self.msg_count() == 0 or len(self.to_ack) >= self.thread.maxsize:
                self.process()
                self.wait_time = 0
                self.can_process = False
                if self.end:
                    try:
                        name = self.thread.queue.name
                        self.consumer.cancel()
                        self.thread.queue.delete()
                        self.should_stop = True
                        log.debug("Queue {} cleaned successfully".format(name))
                    except Exception as e:
                        log.exception(e)

    def process(self):
        for body, msg in self.to_ack:
            msg.ack()
        self.to_ack = []

    def on_iteration(self):
        while not self.can_process:
            if not self.thread.main.is_alive():
                self.should_stop = True
                break
            total = self.msg_count() + len(self.to_ack)
            if total > 0:
                if total >= self.thread.maxsize:
                    self.wait_time = self.thread.timer
                else:
                    self.wait_time += 1
            else:
                self.wait_time = 0
            if self.wait_time >= self.thread.timer:
                time.sleep(random()*5)
                new_total = self.msg_count() + len(self.to_ack)
                if total == new_total or new_total >= self.thread.maxsize:
                    self.can_process = True
                    break
                else:
                    self.wait_time = 0
            time.sleep(1)
        else:
            self.try_process()

    def msg_count(self):
        _, msg_count, _ = self.consumer.channel.queue_declare(self.thread.queue.name, passive=True)
        return msg_count
