.. _integration-script:

======================
Script (output)
======================

Overview
========

The **script** plugin is an in-process Notifier that executes a local
command on the Snooze server when an alert matches a Notification rule
that references a script action.

The command is exec'd directly (not through a shell), so the ``command``
field must be a full argument vector. Callers who need shell semantics
(pipes, redirections, globs) should spell the command as
``["sh", "-c", "..."]`` explicitly.

The alert record is made available to the child process in three ways:

- **stdin** — by default, the JSON-encoded record is fed to stdin
  (matching the Python 1.x ``json: true`` behaviour). The template can
  be changed or cleared.
- **argv** — each element of ``command`` after the program name is a Go
  template rendered over the record, so field values can appear as
  positional arguments.
- **environment variables** — the ``env`` map adds variables to the
  child environment, and variable values are also Go templates.

A hard timeout (default 10 s) is enforced. The combined stdout+stderr is
captured (capped at 64 KiB by default) and included in the error message
when the command fails, making debugging straightforward from the
snooze-server logs.

Configuration
=============

Wire the plugin through a **Notification → Action** in the Snooze UI or
configuration file. Set the action type to ``script`` and fill the
``action_form`` fields described below.

Action fields
-------------

.. list-table::
   :widths: 20 14 18 48
   :header-rows: 1

   * - Field
     - Component
     - Default
     - Description
   * - ``command``
     - Arguments
     - *(required)*
     - Command to execute as an argv list. The first element is the
       program (absolute path or name resolved via ``$PATH``). Each
       element is a Go ``text/template`` rendered against the record
       context (e.g. ``{{ .Record.Host }}``).
   * - ``cwd``
     - String
     - *(optional)*
     - Working directory for the child process. May be a Go template.
       Defaults to the Snooze server's working directory.
   * - ``env``
     - Object
     - *(optional)*
     - Extra environment variables prepended to the parent environment.
       Values are Go templates. User-set values take precedence over
       inherited ones when names collide.
   * - ``stdin``
     - Text
     - ``{{ .RecordJSON }}``
     - Template rendered and fed to the child process on stdin. The
       default sends the full JSON-encoded record (equivalent to the
       Python 1.x ``json: true`` option). Clear this field to send
       nothing on stdin.
   * - ``timeout``
     - Number
     - ``10``
     - Hard wall-clock timeout for the child process in seconds. The
       process is killed when the timeout expires.
   * - ``max_output``
     - Number
     - ``65536``
     - Cap on the combined stdout+stderr captured by the server, in
       bytes. Output beyond this limit is truncated and marked with
       ``... [truncated]``.
   * - ``batch``
     - Switch
     - ``false``
     - When enabled, multiple matching alerts are accumulated and the
       command is invoked once per flush with the first record's
       argv/cwd/env. Stdins are joined as a JSON array (when each parsed
       stdin is valid JSON) or with newlines otherwise. See
       `Batching`_ below.
   * - ``batch_maxsize``
     - Number
     - ``100``
     - Flush the batch when it reaches this many alerts.
   * - ``batch_timer``
     - Number
     - ``10``
     - Flush the batch when it is at least this many seconds old.

Template context
----------------

All ``command`` elements, ``cwd``, ``env`` values, and ``stdin`` are
rendered as Go ``text/template`` with the following dot context:

- ``.Record`` — the full alert record. All standard fields
  (``.Record.Host``, ``.Record.Severity``, ``.Record.Message``, etc.)
  and extra fields (``.Record.Extra``) are accessible.
- ``.Payload`` — the ``NotificationPayload`` carrying the notification
  subject and body (if set by the notification rule).
- ``.RecordJSON`` — the JSON-encoded record as a string. Used by the
  default ``stdin`` template.

Batching
--------

All three of ``batch``, ``batch_maxsize``, and ``batch_timer`` must be
configured for batching to activate. If either bound is absent or
non-positive the plugin falls back to one invocation per alert.

The batch flush runs the command once using the first queued record's
argv, cwd, and env. The stdin payload is a JSON array of individual
stdins when every stdin template renders as valid JSON (the common case
when using the default ``{{ .RecordJSON }}``) or a newline-joined
concatenation otherwise.

Example
=======

.. code-block:: yaml
   :caption: Send a Pushover notification via a shell helper

   command:
     - "/usr/local/bin/pushover-notify"
     - "{{ .Record.Severity }}"
     - "{{ .Record.Host }}"
     - "{{ .Record.Message }}"
   timeout: 15

.. code-block:: yaml
   :caption: Pipe the JSON record to a Python processor

   command:
     - "python3"
     - "/opt/snooze/handlers/escalate.py"
   stdin:   "{{ .RecordJSON }}"
   timeout: 30

.. code-block:: yaml
   :caption: Pass the record via environment variables (no stdin)

   command:
     - "/opt/snooze/handlers/ticket-open.sh"
   env:
     SNOOZE_HOST:     "{{ .Record.Host }}"
     SNOOZE_SEVERITY: "{{ .Record.Severity }}"
     SNOOZE_MESSAGE:  "{{ .Record.Message }}"
   stdin:   ""
   timeout: 20

.. code-block:: yaml
   :caption: Batch multiple alerts into one invocation

   command:
     - "/opt/snooze/handlers/bulk-ingest.py"
   batch:         true
   batch_maxsize: 20
   batch_timer:   60

Testing / verifying
===================

1. **Create a test action** in the Snooze UI (Actions → New → script)
   with ``command`` pointing at a simple script that logs its
   arguments and stdin, for example::

      #!/bin/sh
      echo "argv: $*" >> /tmp/snooze-test.log
      cat >> /tmp/snooze-test.log

2. **Create a matching Notification rule** that routes a specific
   condition (e.g. ``source = "test"``) to the new action.

3. **Send a test alert**::

      $ snooze alert source=test host=test-host severity=info \
          "message=script notifier smoke-test"

4. **Check the log file** ``/tmp/snooze-test.log`` for the rendered
   arguments and the JSON record on stdin.

For timeout and error-path testing, use a script that sleeps or exits
non-zero, then inspect the snooze-server logs for the ``ExecError``
message including the captured output.

Notes & limitations
===================

- The command is exec'd directly via ``exec.CommandContext``, never
  through ``sh -c``. Shell metacharacters in argument templates (pipes,
  redirections, wildcards) are passed literally to the program as argv
  elements. Use ``["sh", "-c", "..."]`` explicitly if you need a shell.
- A non-zero exit code, a process timeout, or an exec-time failure
  (e.g. command not found) all produce an error. The combined
  stdout+stderr (up to ``max_output`` bytes) is included in the error
  message logged by the notification worker.
- ``WaitDelay`` (250 ms) is applied after the process exits: grandchild
  processes that inherited the stdout/stderr pipes (e.g. background jobs
  spawned by a shell wrapper) are waited on for at most 250 ms before the
  server moves on.
- The server process's environment is inherited by the child, plus any
  ``env`` overrides. Sensitive variables in the server environment are
  visible to the script. Use ``env`` to scope the child's environment
  explicitly if isolation matters.
- Batching joins stdins into a JSON array only when every individual
  stdin renders as valid JSON. If the ``stdin`` template produces a
  non-JSON string, batched stdins are joined with newlines instead.
