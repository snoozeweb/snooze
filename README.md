# Snooze

## Command line

Running a snooze server:
```bash
snooze-server &
```

Obtain a root token from the socket of a runnning snooze:
```bash
snooze root-token
```

Configure bash completion at system level:
```bash
_SNOOZE_COMPLETION=source_bash snooze > /etc/bash_completion.d/snooze.sh
```

Configure bash completion at user level:
```bash
mkdir -p ~/.bash_completion.d
_SNOOZE_COMPLETION=source_bash snooze > ~/.bash_completion.d/snooze.sh
cat <<END > ~/.bash_completion
for bcfile in ~/.bash_completion.d/*; do
    . $bcfile
done
END
```
> Note: You might need to adapt to your `.bashrc`/`.bash_completion` existing
> files.

## API

### Write to API

```bash
curl localhost:8080/record/ \
    -X POST \
    -H 'Content-Type: application/json' \
    -d '{"a": "1", "b": "2"}'
```

`Content-Type` matters.

### Retrieve from API

```bash
curl localhost:8080/record/ \
    -H 'Content-Type: application/json' \
    -d '[]' # search here
```

Search example:
```json
[
    "and",
    ["=", "a", "1"],
    ["=", "b", "2"]
]
```
