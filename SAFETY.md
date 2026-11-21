# Safety, Durability, and Trust Boundaries

Shmorby is a local, single-user, single-process tool. This document
states what survives a crash or signal, which operations block, what
the approval and execution records guarantee, and where the audit,
redaction, and process-isolation boundaries end.

It describes current behavior. Items marked **Planned** are not
implemented. See [HOW_THIS_WORKS.md](HOW_THIS_WORKS.md) for the
architecture and [README.md](README.md) for configuration.

## 1. Deployment assumptions

The supported deployment is one operating-system user running one
interactive `shmorby` process against one local data directory.

- The agent runs with the invoking user's full privileges. It is not
  a sandbox, container boundary, or privilege-separation mechanism.
- Prompts, approvals, and permission rules are cooperative safety
  controls. They are not a defense against a hostile local process or
  a user who can edit configuration and databases.
- Subagents run inside the same process. They share the process,
  provider credentials, SQLite handles, and audit logger.
- There is no daemon and no network listener. Closing the process
  stops the agent loop.
- The LLM provider is external. Conversation text, tool output after
  redaction, and images are sent to that provider.
- SQLite provides concurrent readers and a single writer through WAL
  and a busy timeout. A second agent instance and mutating CLI
  commands are not coordinated with a live agent; two processes
  resuming the same session are last-writer-wins per message
  sequence.
- The ledger CLI is the exception: it takes an OS file lock around
  each read-modify-write. That lock does not cover sessions, audit,
  or executions.

## 2. Where state is stored

Default paths are shown in [README.md](README.md). Session, audit,
and memory paths have configuration overrides. Ledger and image paths
follow the XDG data directory.

### Session and execution data (`sessions.db`)

- Session metadata (`sessions`): title, directory, agent mode,
  provider, model, timestamps, and archive state. It survives process
  exit.
- Conversation history (`session_messages`): redacted on disk and
  written in one transaction per completed turn. It survives process
  exit.
- Image attachments (`images/<session-id>/` under the data
  directory): raw pixels in `0600` files inside `0700` directories.
  They survive process exit and are never redacted.
- Executions (`executions`): created before permission evaluation.
  Terminal status may be missing after a crash. They survive process
  exit.
- Execution output (`execution_output`): redacted, 32 KiB chunks,
  1 MiB cap. It survives process exit when the finish path completes.
- Approval requests (`approvals`): written before prompting and
  resolved before execution. They survive process exit.

### Audit, memory, and ledger

- Audit records (`audit.db`): tool runs, output hashes, permission
  decisions, lifecycle events, and subagent records. Retention is
  manual through `audit vacuum`.
- Audit chain (`audit.db`, `audit_chain`): an unkeyed SHA-256 chain.
  See section 7.
- Memory entries (`memory.db` and vectors): best-effort derived data,
  not a safety record.
- Ledger (`ledger.json.age` plus `ledger.key`): encrypted at rest,
  written atomically, and protected by a file lock.
- Configuration YAML: may contain provider API keys in plaintext.
- `shmorby.log`: diagnostic only, not a complete audit record.

### Process-only state

- Backend events are an in-process pub/sub channel. They are bounded
  and lossy for slow subscribers.
- Permission allow-all grants are scoped to one turn.
- Active execution state is reconstructed from SQLite and reconciled
  at next startup.

`sessions.db` is created with mode `0600`. SQLite creates `audit.db`
and `memory.db` under the process umask; shmorby does not chmod them.
Those databases are not encrypted and rely on filesystem permissions.
The ledger is the only encrypted store. Treat all of these files,
including backups, as sensitive operational data.

With `session.enabled: false`, the session store is absent. Session
history, executions, and approvals are then not persisted, and restart
recovery is unavailable. Approval timeouts still deny.

## 3. Synchronous and asynchronous work

"Synchronous" here means the caller waits for the write to finish. It
does not mean every write adds a separate filesystem flush barrier.

- **Completed session turn:** synchronous SQLite transaction under
  the session mutex. A failed write is logged; the in-memory turn
  remains authoritative until process exit.
- **Session metadata on exit:** synchronous `Sync()` before store
  close. A failed write is logged and runtime mode/model changes may
  be lost.
- **Input idempotency:** unique `(session_id, input_id)` index and
  database lookup. A duplicate input returns the stored reply and
  does not start a second turn.
- **Execution create/update:** synchronous SQLite writes at lifecycle
  boundaries. A failed write is logged; the operation may continue
  with an unpersisted state.
- **Approval request/decision:** synchronous SQLite transaction.
  Failure denies the execution.
- **Approval timeout:** five-minute timer; timeout denies and persists
  `expired`/`reject` when the store is writable.
- **Audit lifecycle and permission events:** synchronous write in both
  delivery modes. `required` fails the operation; `best_effort` logs
  a drop.
- **Audit tool run/output and subagent events:** asynchronous buffer
  in `best_effort`. Events are dropped when the buffer is full or the
  process dies. `required` writes synchronously.
- **Audit buffer close:** drains and flushes pending events. Store
  errors are logged and counted.
- **Backend events:** non-blocking publish. Slow subscribers lose the
  oldest queued event.
- **Ledger writes:** synchronous encrypted write with a file lock.
  The caller receives an error on failure.
- **Memory capture:** synchronous insert and embedding work. A failed
  write is discarded; the tool result is still returned.

In best-effort mode, the audit buffer defaults to 100 events and
flushes every 500 ms or when 100 tool runs accumulate. A normal
shutdown drains it. An abrupt death does not.

## 4. Shutdown, crash, and signal behavior

- **`/quit` or EOF:** stops the UI loop. Session metadata is synced,
  the audit buffer is flushed, and the stores are closed.
- **`SIGINT` / `SIGTERM`:** cancels the root context. Active provider
  calls, tool calls, and approvals observe cancellation. Cancelled
  command groups are killed. The TUI exits through Bubble Tea's signal
  handling. The REPL checks cancellation between inputs; an idle REPL
  blocked on stdin may not exit immediately. When a frontend returns,
  normal shutdown defers sync session metadata and flush the audit
  buffer.
- **Panic or fatal runtime error:** no deferred cleanup is guaranteed.
  Previously committed rows remain. The current turn, buffered audit
  events, and in-progress state transitions may be missing. Child
  processes may survive.
- **`SIGKILL`:** no handler runs. This is the same as a crash. SQLite
  recovers its WAL at next open. Child process groups can survive.
- **Host crash or power loss:** the process and children are gone.
  Durability depends on SQLite, the filesystem, and storage hardware.
  Shmorby has no application-level backup, replication, or fsync
  barrier beyond SQLite.

A completed session turn is written to SQLite before the turn is
considered complete. An interrupted turn remains only in memory and
is lost on an abrupt exit. On resume, a trailing assistant tool-call
message without matching tool results is truncated from both the
loaded history and the store.

The main signal handler unregisters after the first `SIGINT` or
`SIGTERM`, so a later signal can use the operating system's default
action and may terminate without running shutdown defers.

## 5. Approval guarantees and edge cases

Every interactive `ask` decision follows this order:

1. Create the execution row with `pending_approval`.
2. Persist the approval request, linked to that execution.
3. Prompt the local user.
4. Persist the decision before any tool starts.
5. Start the tool only if the decision is `once` or `always`.

The default timeout is five minutes and is not configurable in the
current version. An unanswered prompt is denied, not allowed. The
approval row becomes `expired`; the execution becomes `denied`.

Interactive decisions are recorded with the local OS username; system
decisions use `system`. Every decision also records a decision source
and scope. `always` applies only to subsequent calls in the current
turn. It is not a durable permission rule and does not survive a new
turn or process restart.

Edge cases:

- **Cancellation while prompting.** Context cancellation rejects the
  request and records `decision_source=cancelled`.
- **Closed prompt channel.** A disconnected prompt rejects the request
  and records `decision_source=disconnected`.
- **Persistence failure.** If the request cannot be stored, or the
  decision cannot be stored, the execution is denied. Shmorby never
  proceeds on an unpersisted allow.
- **Audit failure in required mode.** If the approval request or
  decision cannot be audited, the request is denied before execution.
- **Restart.** At startup, pending approvals whose owning process is
  no longer alive are expired and their executions denied with
  `decision_source=restart`. Pending approvals owned by a live
  shmorby process are left alone.
- **Second process.** Approval decisions are delivered only to the
  in-process approval service. There is no cross-process approval
  inbox or CLI decision command. A request owned by another live
  process cannot be answered from this process.
- **Unattended use.** The implemented TUI and REPL always install an
  interactive prompt handler. There is no supported unattended mode
  that turns `ask` into a durable allow.
- **Non-interactive configuration.** `permission.interactive` is
  exposed as a configuration and `/set` value, but the current
  TUI/REPL prompt path remains interactive. Do not rely on it as a
  non-interactive approval policy.

## 6. Execution and cancellation guarantees

An execution is created before permission evaluation and can be in
one of these states:

`pending_approval`, `denied`, `running`, `succeeded`, `failed`,
`cancel_requested`, `cancelled`, `kill_failed`, `timed_out`,
`orphaned`, `unknown`.

Each row records the tool, redacted command and arguments, a
canonical argument hash, transcript input/tool-call IDs, output
reference, and the owning shmorby process identity.

Cancellation targets an execution ID:

1. Set `cancel_requested` and persist it.
2. Cancel the tool context.
3. Kill the process group with `SIGKILL` on Unix or
   `taskkill /T /F` on Windows.
4. When a PID was recorded, confirm the group is gone.
5. Record `cancelled`. If a recorded group is still alive, record
   `kill_failed` with the PID and diagnostic.

Cancellation does not send a graceful `SIGTERM` first. The shell tool
and the shared executor used by ssh, sudo, and aws record the
process-group leader PID. Their executions can therefore be
reconciled and have cancellation confirmed from the process group.

### Orphans and startup reconciliation

Shmorby intentionally does not use parent-death signals: Go can
deliver them while the creating thread exits even though shmorby is
still running.

- On `SIGINT` or `SIGTERM`, active command groups are killed by the
  normal cancellation path.
- On a crash or `SIGKILL`, child process groups are not killed. They
  can continue running after shmorby is gone.
- At startup, and with `shmorby session executions --reconcile`,
  non-terminal rows are reconciled. Rows owned by a live shmorby
  instance are skipped.
- A row becomes `orphaned` only when its recorded process start
  identity still matches a live process group. A missing, changed, or
  recycled identity becomes `unknown`.
- Reconciliation never marks a row `succeeded` and never kills a
  process by default.
- `shmorby session executions --reconcile --kill` opts into killing
  verified orphaned groups. It refuses to signal a PID whose identity
  no longer matches.

Limitations:

- Orphan detection is based on the process group leader identity.
  On Windows only the leader is checked; child processes are not
  independently verifiable.
- If the recorded group leader exits while descendants remain, its
  identity can no longer be verified and the row becomes `unknown`.
- After the parent dies, no process is left to capture the child's
  output or exit status. Reconciliation reports liveness, not a
  trustworthy exit code.
- There is no supervisor or service manager restart policy.

## 7. Audit delivery and tamper evidence

Audit records are stored separately from sessions in `audit.db`.
They include tool runs, output hashes, permission decisions,
approval and execution lifecycle events, and subagent records.

### Delivery modes

`audit.delivery` accepts:

- **`best_effort` (default).** Lifecycle and permission events are
  attempted synchronously, but a failed write is logged and the
  operation continues. Tool-run output and subagent records are
  buffered asynchronously and may be dropped when the buffer is full
  or the process exits abruptly. Drops are counted and reported in
  the log.
- **`required`.** Shmorby refuses to start if the audit database
  cannot be opened. Lifecycle, permission, tool-run, output, and
  subagent writes are synchronous. A pre-action write failure stops
  the operation; a post-action write failure is returned as an
  error.

`audit.enabled: false` disables audit entirely. It is rejected at
startup when delivery is `required`.

### Hash chain

Each chained record has a monotonic sequence, a canonical JSON
payload, the previous record's hash, and its own SHA-256 hash. The
chain includes tool runs, permission decisions, approval and
execution lifecycle events, output hashes, and subagent
start/finish events. `shmorby audit verify` verifies the full chain
or an inclusive `--from`/`--to` range and reports the first invalid
sequence.

`audit vacuum` removes old rows from the query tables but retains
chain history. Captured stdout and stderr are represented in the
chain by hashes, not copied into it.

### Limits

The chain detects accidental or casual modification: edited rows,
removed or reordered chain entries, broken links, and mismatches
between chain payloads and source rows while those source rows
exist.

It does not prevent modification. In particular:

- The chain is unkeyed. There is no signature and no externally
  held secret.
- There is no append-only or remote copy. Anyone who can write the
  database can rewrite the chain and its state, or replace the file.
- Truncating the chain and rewriting `audit_chain_state` is not
  detectable without an external checkpoint.
- Deleting source rows is not treated as tampering because
  `audit vacuum` intentionally does so.
- Records written before the chain was introduced have no
  retroactive integrity protection.
- Best-effort delivery can lose records before they reach the chain.
- Audit timestamps come from the local host clock.
- The `audit` CLI uses `audit.DefaultDBPath()` under `$HOME`,
  ignoring both a custom `audit.db_path` and `XDG_DATA_HOME`.
- `audit.retention_days` is not applied automatically; retention runs
  only when an operator invokes `audit vacuum`.

See section 10 for what would be required before treating this as
compliance-grade evidence.

## 8. Secret redaction boundaries

Redaction is a best-effort defense in depth. It matches known token
patterns and JSON keys whose names look secret-bearing. It is not a
secret classifier and does not guarantee that every credential is
removed.

- **Tool output shown in the UI:** not redacted. The terminal can
  contain raw secrets and scrollback.
- **Tool output returned to the LLM:** redacted. Known patterns are
  replaced before the next provider call.
- **Tool-run audit args and output:** redacted. Pattern-based; output
  capture is capped.
- **Permission audit and approval commands:** redacted. JSON-aware key
  redaction plus patterns.
- **Execution command, args, error, output:** redacted. Raw process
  memory still holds the original.
- **Session message content:** redacted on disk. Live memory keeps raw
  text; only disk copies are scrubbed.
- **Session title:** not redacted. It is derived from the first user
  message and stored as-is.
- **Memory result:** redacted, pattern-based.
- **Memory command and args:** not redacted. Stored as captured and
  can contain secrets.
- **Ledger values written by the agent:** redacted, and encrypted at
  rest.
- **Clipboard images:** not redacted. Pixels are sent and stored
  verbatim.
- **Config files and provider API keys:** not redacted. Plain YAML;
  protect file permissions.
- **Shmorby logs:** not guaranteed. Do not treat them as a
  secret-free record.
- **Provider requests:** conversation and raw images. Redaction only
  applies to tool output returned to the model.

Other limits:

- The generic `api_key`, `secret`, and `password` patterns require a
  recognizable assignment form and a value of at least eight
  characters.
- Encoded, split, transformed, or unknown credential formats can
  pass through.
- Redaction does not remove hostnames, paths, internal IPs, command
  output, or other operationally sensitive context.
- Only the ledger is encrypted at rest. Sessions, audit, memory,
  images, logs, and configuration are not.
- Redaction does not protect the live terminal, screen recordings,
  shell history, or process memory.

## 9. Why client/server is deferred

The current design is deliberately a single binary with an
in-process backend. That keeps credentials, process access, and
SQLite files on the local host, and avoids deploying and protecting
a network service before the domain model is sound.

The `internal/backend` seam and its event contract are in-process
only. They are not an HTTP or SSE API. Events are not durable and
are lost on restart. Approvals and executions are persisted, but
they are resolved by the process that created them.

This is deferred because a client/server split would add problems
that do not exist locally:

- authentication, pairing, TLS, and loopback/default-safe binding
- per-client authorization and secret isolation
- multi-user identity and approver attribution
- durable, resumable event delivery
- cross-process locking and execution ownership
- API versioning and compatibility
- a larger remote attack surface for a tool that already has
  privileged local execution

Product triggers for a future split:

- Approvals or monitoring must work from a different device or
  process while the agent runs.
- Multiple users or teams need isolated sessions and audit views.
- Shmorby must run as an always-on service with no interactive UI.
- Automation needs a supported API instead of driving the CLI.
- Several clients must attach to the same live session.
- Audit or execution records must be centralized off-host.
- Remote hosts need to be managed through a central control plane
  rather than local shell and SSH.

Before that split, a transport still needs a durable event log with
a cursor, a reviewed API contract, authentication and pairing,
authorization for approvals and cancellation, TLS for any non-local
binding, and tests proving the in-process backend and API adapters
have identical safety behavior. The in-process backend should remain
the default until those exist.

## 10. What compliance-grade audit would require

The current audit trail is useful for local incident review and
operator accountability. It is not compliance-grade and must not be
described as such.

Before making that claim, at minimum:

- Make synchronous, fail-closed delivery the default for all
  privileged actions, including denials, cancellation, configuration
  changes, and child-process outcomes.
- Anchor the chain outside the writable database, for example with
  signed checkpoints, an append-only remote log, or WORM storage.
- Sign records with a key that is separated from database write
  access, and document key rotation and custody.
- Provide a trusted time source rather than relying only on the
  local clock.
- Guarantee complete coverage and stable identifiers for sessions,
  inputs, approvals, executions, audit records, and subagent
  lineage.
- Define retention, legal hold, export, backup, restore, and
  independent verification procedures.
- Restrict and audit access to the audit store itself; an
  administrator who can rewrite the database must not be the only
  control.
- Test tamper detection against deletion, truncation, replacement,
  and full-chain recomputation.
- Have the design and operating procedure reviewed independently and
  produce evidence suitable for the intended standard.

That is a product and operational program, not only a code change.

## 11. Current versus planned

Current:

- In-process TUI/REPL backend with a volatile event stream
- Durable sessions, executions, approvals, and hash-chained audit
  records in local SQLite
- Default-deny approval timeout and restart recovery
- Execution-scoped cancellation with process-group confirmation
- Startup and CLI reconciliation of orphans; opt-in kill
- Best-effort audit by default, required mode available
- Best-effort redaction with local filesystem permissions

Planned or under consideration:

- A local HTTP API with authenticated clients and a durable event log
- Remote approval and monitoring clients
- Multi-user identity and authorization
- Further session UI work such as pickers, forking, and export
- Stronger external audit anchoring and compliance evidence

## 12. References

- [README.md](README.md) — configuration, commands, known limitations
- [HOW_THIS_WORKS.md](HOW_THIS_WORKS.md) — architecture and data flow
- `internal/approval/service.go` — approval request, timeout, decision
- `internal/execution/manager.go` — execution lifecycle and cancel
- `internal/execution/execution.go` — reconciliation and statuses
- `internal/session/approval_store.go` — approval persistence
- `internal/session/execution_store.go` — execution persistence
- `internal/audit/logger.go` — delivery modes and buffering
- `internal/audit/store.go` — hash chain and verification
- `internal/redact/redact.go` — redaction patterns and boundaries
- `internal/exec/exec.go` — process-group isolation
- `internal/backend` — in-process event contract
