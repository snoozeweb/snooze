from snooze.utils.functions import dig, sanitize, flatten

def test_dig():
    dic = {
        'a': {
            'b': {
                'c': 'found'
            }
        }
    }
    assert dig(dic, 'a', 'b', 'c') == 'found'

def test_sanitize():
    dic = {'a.b': {'c.d': 0}}
    assert sanitize(dic) == {'a_b': {'c_d': 0}}

def test_flatten():
    a = [1,[2,[3,[4,[5]]]]]
    assert flatten(a) == [1, 2, 3, 4, 5]
