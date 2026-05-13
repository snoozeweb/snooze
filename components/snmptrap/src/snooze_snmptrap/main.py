'''SNMPTrap input plugin for Snooze'''

import asyncio
import datetime
import logging
import os
import yaml
from multiprocessing import JoinableQueue
from threading import Thread
from pathlib import Path

from pysnmp.carrier.asyncio.dgram import udp
from pysnmp.entity import engine, config
from pysnmp.entity.rfc3413 import ntfrcv
from pysnmp.smi.rfc1902 import ObjectIdentity
from pysnmp.smi import view
from pysnmp.proto.rfc1902 import *
from pysnmp.smi import view, compiler, builder
from pysnmp.smi.error import MibNotFoundError, NoSuchObjectError
from pysnmp.proto.api import v2c

from snooze_client import Snooze

log = logging.getLogger("snooze.snmptrap")
logging.basicConfig(
    format="%(asctime)s - %(name)s: %(levelname)s - %(message)s",
    level=logging.DEBUG,
)

MAP_TABLE = {
}


class SNMPTrap:
    def __init__(
        self,
        queue,
        bind_address="0.0.0.0",
        port=162,
        mib_dirs=None,
        mib_list=None,
        community="public",
        v3_users=None,
    ):
        self.queue = queue
        self.mib_dirs = mib_dirs or ["/usr/share/snmp/mibs"]
        self.mib_list = mib_list or []

        self.loop = asyncio.new_event_loop()
        asyncio.set_event_loop(self.loop)

        self.snmp_engine = engine.SnmpEngine()

        config.addTransport(
            self.snmp_engine,
            udp.domainName,
            udp.UdpTransport().openServerMode((bind_address, port)),
        )

        # SNMPv1/v2c community string
        config.addV1System(self.snmp_engine, "my-area", community)

        # SNMPv3 users
        v3_users = v3_users or []
        for user in v3_users:
            username = user.get("username")
            auth_key = user.get("auth_key")
            priv_key = user.get("priv_key")
            auth_protocol = user.get("auth_protocol", "none")
            priv_protocol = user.get("priv_protocol", "none")

            # Map auth protocol
            auth_proto_map = {
                "none": config.usmNoAuthProtocol,
                "md5": config.usmHMACMD5AuthProtocol,
                "sha": config.usmHMACSHAAuthProtocol,
                "sha224": config.usmHMAC128SHA224AuthProtocol,
                "sha256": config.usmHMAC192SHA256AuthProtocol,
                "sha384": config.usmHMAC256SHA384AuthProtocol,
                "sha512": config.usmHMAC384SHA512AuthProtocol,
            }
            # Map priv protocol
            priv_proto_map = {
                "none": config.usmNoPrivProtocol,
                "des": config.usmDESPrivProtocol,
                "3des": config.usm3DESEDEPrivProtocol,
                "aes": config.usmAesCfb128Protocol,
                "aes128": config.usmAesCfb128Protocol,
                "aes192": config.usmAesCfb192Protocol,
                "aes256": config.usmAesCfb256Protocol,
            }

            auth_proto = auth_proto_map.get(auth_protocol.lower(), config.usmNoAuthProtocol)
            priv_proto = priv_proto_map.get(priv_protocol.lower(), config.usmNoPrivProtocol)

            log.info("Adding SNMPv3 user: %s (auth=%s, priv=%s)", username, auth_protocol, priv_protocol)

            # For SNMPv3 TRAP reception with no authentication, we use the magic
            # securityEngineId of five zeros to accept traps from any engine ID.
            # See pysnmp documentation for UsmUserData.
            if auth_proto == config.usmNoAuthProtocol:
                # Use OctetString for the magic engine ID (five zeros)
                from pysnmp.proto.rfc1902 import OctetString
                magic_engine_id = OctetString(hexValue='0000000000')
                config.addV3User(
                    self.snmp_engine,
                    username,
                    auth_proto, auth_key,
                    priv_proto, priv_key,
                    securityEngineId=magic_engine_id,
                )
            else:
                config.addV3User(
                    self.snmp_engine,
                    username,
                    auth_proto, auth_key,
                    priv_proto, priv_key,
                )

        ntfrcv.NotificationReceiver(self.snmp_engine, self._cbFun)

        self._load_mibs()

    def _load_mibs(self):
        snmp_builder = builder.MibBuilder()
        snmp_view = view.MibViewController(snmp_builder)
        mib_dirs = [f"file:{path}" for path in self.mib_dirs]
        compiler.addMibCompiler(snmp_builder, sources=mib_dirs)
        snmp_builder.loadModules(*self.mib_list)
        self.view = snmp_view

    def _cbFun(self, snmp_engine, state, context_id, context_name, var_binds, cbctx):
        try:
            exec_ctx = snmp_engine.observer.getExecutionContext(
                "rfc3412.receiveMessage:request"
            )
            source_ip, _ = exec_ctx["transportAddress"]
            log.debug("Trap received from %s", source_ip)

            record = self._handler(var_binds)
            record["source_ip"] = source_ip
            record["source"] = "snmptrap"
            record["timestamp"] = datetime.datetime.now().astimezone().isoformat()

            self.queue.put(record)

        except Exception as err:
            log.warning("Trap processing failed: %s", err)

    def _handler(self, oids):
        record = {}
        for oid, value in oids:
            key, val = self._process_mib(oid, value)
            if key and val is not None:
                record[key.replace(".", "_")] = val
        return record

    def _process_mib(self, oid, value):
        try:
            identity = ObjectIdentity(oid).resolveWithMib(self.view)

            module = identity.getMibSymbol()[0]
            symbol = identity.getMibSymbol()[1]
            indices = identity.getMibSymbol()[2] or []
    
            # Special case: snmpTrapOID
            if (module, symbol) == ("SNMPv2-MIB", "snmpTrapOID"):
                trap_id = ObjectIdentity(value).resolveWithMib(self.view)
                trap_module, trap_symbol, _ = trap_id.getMibSymbol()
                return "oid", f"{trap_symbol}::{trap_module}"
    
            name = f"{module}::{symbol}"
            for suffix in indices:
                name += f".{suffix}"
    
            # Better handling of OctetString values - try to decode as UTF-8
            pretty_value = self._decode_value(value)
            return name, pretty_value
    
        except Exception as err:
            log.warning("Could not resolve OID %s: %s", oid, err)
            return str(oid), self._decode_value(value)

    def _decode_value(self, value):
        """Try to decode SNMP values to human-readable strings"""
        # For OctetString, try to decode as UTF-8
        if hasattr(value, 'hasValue') and value.hasValue():
            if isinstance(value, OctetString):
                try:
                    # Get raw bytes and try UTF-8 decoding
                    raw_bytes = bytes(value)
                    return raw_bytes.decode('utf-8')
                except (UnicodeDecodeError, TypeError):
                    # Fall back to prettyPrint if UTF-8 decoding fails
                    return value.prettyPrint()
        return value.prettyPrint()

    def start(self):
        asyncio.set_event_loop(self.loop)
        self.snmp_engine.transportDispatcher.jobStarted(1)
        log.info("SNMP Trap listener started")
        self.loop.run_forever()

    def stop(self):
        self.snmp_engine.transportDispatcher.jobFinished(1)
        self.loop.stop()

def snmp_map(record):
    '''Map certain common SNMPTrap OIDs to field names used by Snooze'''
    for key, value in record.items():
        log.debug("Mapping %s, %s", key, value)
        # Mapping SNMP types to JSON serializable types
        value_type = type(value)
        if value_type == Null:
            value = None
        elif value_type in [Integer, Integer32, Unsigned32, Gauge32, Counter64]:
            value = int(value)
        elif value_type in [OctetString, Opaque]:
            value = str(value)
        elif value_type == Bits:
            value = value.pretty_print()
        elif value_type == IpAddress:
            value = str(value)
        elif value_type == ObjectIdentifier:
            value = str(value)
        elif value_type == TimeTicks:
            # Change the timetick to seconds
            value = int(value) / 100
        else:
            value = str(value)

        record[key] = value

        log.debug("New value: %s", value)

        # Mapping SNMP OIDs to fields used by Snooze
        if key in MAP_TABLE:
            new_key = MAP_TABLE[key]
            log.debug("Mapping %s=>%s", key, new_key)
            record[new_key] = value

    return record

class Main:
    def __init__(self):
        # config
        self.config = {}

        config_file = os.environ.get('SNOOZE_SNMPTRAP_CONFIG') or '/etc/snooze/snmptrap.yaml'
        config_file = Path(config_file)
        try:
            with config_file.open('r') as myfile:
                self.config = yaml.safe_load(myfile.read())
        except Exception as err:
            log.error("Error loading config: %s", err)

        if not isinstance(self.config, dict):
            self.config = {}

        snooze_uri = self.config.get('snooze_server', None)
        self.api = Snooze(snooze_uri)

        self.send_workers_pool = self.config.get('send_workers', 4)

        listening_address = self.config.get('listening_address', '0.0.0.0')
        listening_port = self.config.get('listening_port', 162)
        mib_dirs = self.config.get('mib_dirs', ['/usr/share/snmp/mibs'])
        community = self.config.get('community', 'public')
        v3_users = self.config.get('v3_users', [])

        self.send_queue = JoinableQueue()
        self.snmp_server = SNMPTrap(
            self.send_queue,
            bind_address=listening_address,
            port=listening_port,
            mib_dirs=mib_dirs,
            mib_list=[],
            community=community,
            v3_users=v3_users,
        )
        self.snmp_thread = Thread(target=self.snmp_server.start, daemon=True)

    def start_send_workers(self, worker_pool):
        threads = []
        for index in range(worker_pool):
            mythread = Thread(target=self.send_worker, args=(index,))
            mythread.start()
            threads.append(mythread)
        return threads

    def send_worker(self, index):
        '''A worker for sending records to Snooze'''
        while True:
            log.debug("[send_record] Waiting for queue")
            record = self.send_queue.get()
            if not record:
                log.info("Stopping send worker %d", index)
                break
            snmp_map(record)
            log.debug("Sending record to snooze: %s", record)
            self.api.alert(record)

    def stop_threads(self, queue, threads):
        for _ in threads:
            queue.put(None)
        for thread in threads:
            thread.join()

    def run(self):
        try:
            self.snmp_thread.start()
            send_threads = self.start_send_workers(self.send_workers_pool)

            threads = [self.snmp_thread] + send_threads
            for thread in threads:
                thread.join()
        finally:
            log.info("Stopping SNMP listener")
            transportDispatcher = self.snmp_server.snmp_engine.transportDispatcher
            transportDispatcher.jobFinished(1)
            transportDispatcher.unregisterRecvCbFun(recvId=None)
            #transportDispatcher.unregisterTransport(udp.domainName)
            self.stop_threads(self.send_queue, send_threads)
            self.snmp_server.stop()

def main():
    '''Main function to execute when the script is executed directly'''
    Main().run()

if __name__ == '__main__':
    main()