---
sidebar_position: 11
---

# Key-values

## Overview

Located on the **utils** page of the Web UI, **Key-values** offers a fast and efficient way to store dictionnaries that can be used for mapping alert's specific fields.

Being used as a **Modification** in [Rules](./rules.md#key-value-mapping), **Key-values** are usually used for replacing multiple *condition/set modification* pairs, which not only favors readability but also greatly reduces processing time.

## Web interface

![](./images/web_kv.png)

When more than one dictionary exists, a tab bar appears above the search bar:
an **All** tab that lists every key-value, followed by one tab per dictionary.
Selecting a dictionary tab filters the list down to that dictionary (and still
combines with anything typed in the search bar). With a single dictionary the
tab bar is hidden, since there is nothing to switch between.

Dictionary\*  
Name of the dictionary

Key\*  
Key of the dictionnary

Value\*  
Value to associate with the key

:::warning

The pair (dictionnary, key) being unique by definition, any duplicate entry will overwrite the previous one

:::

