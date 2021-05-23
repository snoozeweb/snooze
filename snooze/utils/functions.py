#!/usr/bin/python3.6

def dig(dic, *lst):
    """
    Input: Dict, List
    Output: Any

    Like a Dict[value], but recursive
    """
    if len(lst) > 0:
        try:
            return dig(dic[lst[0]], *lst[1:])
        except:
            return None
    else:
        return dic

flatten = lambda x: [z for y in x for z in (flatten(y) if hasattr(y, '__iter__') and not isinstance(y, str) else (y,))]
