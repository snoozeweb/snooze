from snooze.utils.functions import dig, flatten

def test_dig():
    dic = {
        'a': {
            'b': {
                'c': 'found'
            }
        }
    }
    assert dig(dic, 'a', 'b', 'c') == 'found'

def test_flatten():
    a = [1,[2,[3,[4,[5]]]]]
    assert flatten(a) == [1, 2, 3, 4, 5]
