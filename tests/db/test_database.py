'''General tests for database objects'''

from snooze.db.database import *

class TestAsyncIncrement:
    def test_init(self, core):
        inc = AsyncIncrement(core.db, 'snooze', 'hits')
        assert inc
    def test_increment(self, core):
        inc = AsyncIncrement(core.db, 'snooze', 'hits')
        inc.increment(dict(name='filter 01'))
        assert inc.increments[(('name', 'filter 01'),)] == 1
        inc.increment(dict(name='filter 01'))
        assert inc.increments[(('name', 'filter 01'),)] == 2
    def test_flush(self, core):
        inc = AsyncIncrement(core.db, 'snooze', 'hits')
        inc.increment(dict(name='filter 01'))
        inc.increment(dict(name='filter 01'))
        inc.flush()
        assert inc.increments[(('name', 'filter 01'),)] == 0

class TestAsyncDatabase:
    def test_init(self, core):
        adb = AsyncDatabase(core.db)
        assert adb
    def test_new_increment(self, core):
        inc = AsyncIncrement(core.db, 'snooze', 'hits')
        adb = AsyncDatabase(core.db)
        adb.new_increment(inc)
        assert adb.increments['snooze'] == inc
