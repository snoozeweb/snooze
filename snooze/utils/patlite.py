'''A module to change patlite lights'''

import time
import struct
import sys

from enum import Enum
from socket import socket, AF_INET, SOCK_STREAM

class Light(Enum):
    '''Light codes for Patlite'''
    RED = 0
    YELLOW = 1
    GREEN = 2
    BLUE = 3
    WHITE = 4

class Pattern(Enum):
    '''Light pattern codes for Patlite'''
    OFF = 0
    ON = 1
    BLINK1 = 2
    BLINK2 = 3

class Buzzer(Enum):
    '''Sound pattern codes for Patlite Buzzer'''
    OFF = 0
    SHORT = 1
    LONG = 2
    TINY = 3
    BEEP = 4

ALERT = 5

READ = b'\x58\x58\x47\x00\x00\x00'
WRITE_HEADER = b'\x58\x58\x53\x00\x00\x06'
ACK = b'\x06'
NAK = b'\x15'

# Light
OFF    = b'\x00' # ________________
ON     = b'\x01' # ----------------
BLINK1 = b'\x02' # ----____----____
BLINK2 = b'\x03' # -_-_____-_-_____

# Buzzer
SHORT = b'\x01' # --__--__--__--__
LONG  = b'\x02' # ----____----____
TINY  = b'\x03' # -_-_____-_-_____
BEEP  = b'\x04' # ----------------

COLORS = ['red', 'yellow', 'green', 'blue', 'white']
STATE_KEYS = [*COLORS, 'alarm']

LIGHT_TO_CODE = {
    'off': OFF,
    'on': ON,
    'blink1': BLINK1,
    'blink2': BLINK2,
}

BEEP_TO_CODE = {
    'off': OFF,
    'short': SHORT,
    'long': LONG,
    'tiny': TINY,
    'beep': BEEP,
}

CODE_TO_BEEP = {v: k for k, v in BEEP_TO_CODE.items() }
CODE_TO_LIGHT = {v: k for k, v in LIGHT_TO_CODE.items() }

class PatliteError(RuntimeError): pass

class State:
    '''Represent the state of Patlite'''
    def __init__(self, **kwargs):
        self.mystate = kwargs
        self.validate()

    def validate(self):
        '''Validate that the state is valid'''
        for state, alias in self.mystate.items():
            if state in COLORS:
                assert alias in ['off', 'on', 'blink1', 'blink2']
            elif state == 'alarm':
                assert alias in ['off', 'short', 'long', 'tiny', 'beep']

    @staticmethod
    def unpack(data):
        '''Unpack a bytestring and return a State object'''
        codes = struct.unpack('6c', data)
        mydict = {}
        for index, state in enumerate(STATE_KEYS):
            code = codes[index]
            if state in COLORS:
                mydict[state] = CODE_TO_LIGHT[code]
            elif state == 'alarm':
                mydict[state] = CODE_TO_BEEP[code]
        return State(**mydict)

    def pack(self):
        '''Create a bytestring that Patlite will understand'''
        data = []
        for state in STATE_KEYS:
            alias = self.mystate.get(state, 'off')
            if state in COLORS:
                code = LIGHT_TO_CODE[alias]
            elif state == 'alarm':
                code = BEEP_TO_CODE[alias]
            data.append(code)
        return struct.pack('6c', *data)

class Patlite:
    def __init__(self, host, port=10000, timeout=10):
        self.host = host
        self.port = port
        self.timeout = timeout

    def __enter__(self):
        self.sock = socket(AF_INET, SOCK_STREAM)
        self.sock.settimeout(self.timeout)
        self.sock.connect((self.host, self.port))
        return self

    def __exit__(self, exc_type, exc_value, traceback):
        self.sock.close()

    def get_state(self):
        '''Get current status from Patlite'''
        data = self.read()
        print("Received data: {}".format(data))
        state = State.unpack(data)
        return state

    def read(self):
        '''Read the raw data for the state'''
        self.sock.sendall(READ)
        data = self.sock.recv(512)
        return data

    def set_full_state(self, state):
        '''Set the full state of the Patlite'''
        print("Need to set state to: {}".format(state))
        data = state.pack()
        print("Sending data: {}".format(data))
        self.sock.sendall(WRITE_HEADER + data)
        ret = self.sock.recv(512)
        if ret == ACK:
            pass
        elif ret == NAK:
            raise PatliteError("Received NAK")
        else:
            raise PatliteError("Unknown return code from Patlite: {}".format(ret))

    def reset(self):
        '''Reset the Patlite state'''
        state = State()
        self.set_full_state(state)

    def set(self, key, value):
        state = State(**{key: value})
        self.set_full_state(state)

    def append(self, key, value):
        state = self.get_state()

def main():
    address = sys.argv[1]
    with Patlite(address) as patlite:
        status = patlite.get_state()
        print(status)
        patlite.set('blue', 'on')
        time.sleep(1)
        patlite.set('white', 'on')
        time.sleep(1)
        patlite.set('red', 'on')

if __name__ == '__main__':
    main()
