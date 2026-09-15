# End-to-end verification against FirecREST v2 (VDLP-50)

Date: 2026-09-15. Server: the project's local demo stack, `f7t-appversion: 2.6.0`,
one system `fakecluster`. Issuer: the demo launcher on `:8025` (it acts as the OIDC
issuer; there is no separate Keycloak in this stack).

This client was written against the v2 **specification** in VDLP-49 and had never
been run against a server. This document records what that first contact found.
It is a record of observations, not a changelog.

## Result

All four commands work. Three defects were found on the way, one in this client and
two in the demo stack. Details below.

| Command | Outcome |
| --- | --- |
| token, OAuth2 client credentials | works |
| `systems` | works — `fakecluster` |
| `submit` | works — HTTP 201, `{"jobId":"14"}` |
| `status` | works, but only after adding the state call (see 2) |
| `download` | works on an explicit output path; fails on the default one (see 4) |

## 1. The submit body used snake_case; the schema is camelCase

The client sent `working_directory`, `standard_output`, `standard_error`. The
published `JobDescriptionModel` names these `workingDirectory`, `standardOutput`,
`standardError`.

FirecREST 2.6.0 accepts **both**. Submitting the same job twice, once each way,
produced two jobs whose scheduler-side metadata was identical:

```
snake_case -> 201 {"jobId":"10"}   meta: 10|snake-test|/home/demo/tmp|/home/demo/tmp/snake.out
camelCase  -> 201 {"jobId":"11"}   meta: 11|camel-test|/home/demo/tmp|/home/demo/tmp/camel.out
```

So this was never going to surface as a failed request — the fields were being
honoured. It matters anyway: acceptance of the snake_case form comes from Pydantic
models that populate by field name as well as by alias, which is an implementation
detail of this server version, not a promise in the contract. A future release that
sets `populate_by_name=False`, or a different v2 implementation, would silently drop
`working_directory` and run every job in the user's home directory instead of the
requested one — a wrong-directory job, not an error.

**Changed**: the struct tags now carry the documented camelCase names. The unit test
that asserted the snake_case wire format was asserting the wrong thing and was
updated with it.

## 2. `/metadata` carries no job state, so `status` could not answer "is it done?"

`GET /compute/{system}/jobs/{id}/metadata` returns exactly:

```json
{"jobs":[{"jobId":"12","script":"…","standardInput":"/dev/null",
          "standardOutput":"/home/demo/vdlp50-rt.out","standardError":"…"}]}
```

`jobId`, `script`, and the three stream paths. No state, no exit code. The issue
asked to poll "until a terminal state", and with only this call there is nothing to
poll — the response is identical for a queued job and a finished one.

State lives on the sibling endpoint `GET /compute/{system}/jobs/{id}`:

```json
{"jobs":[{"jobId":"12","name":"vdlp50","status":{"state":"COMPLETED",
          "stateReason":"None","exitCode":0,"interruptSignal":0}, …}]}
```

**Added**: `Client.Job` reading that endpoint, a `JobState.Terminal()` predicate over
Slurm's terminal states, and `Client.Systems` for `/status/systems`. `status` now
reports state, exit code and terminality alongside the paths. `Terminal()` treats an
unrecognised state as non-terminal, so an unknown state makes a caller poll again
rather than report a wrong outcome.

Note for whoever writes the polling loop: `Terminal()` is the state-based stop
condition, and it is the one to use. `exitCode` is not a substitute — the field is
nullable in the schema, and this run only ever observed jobs that had already
finished, so its value before terminality is unverified here.

## 3. Stack defect: `standardOutput` is concatenated onto the working directory

Submitting an **absolute** `standardOutput` produces a doubled path in the metadata
response:

```
request : workingDirectory=/home/demo/tmp  standardOutput=/home/demo/tmp/snake.out
metadata: "standardOutput": "/home/demo/tmp//home/demo/tmp/snake.out"
```

The file is written to the right place; only the reported path is wrong. The
join is unconditional instead of respecting an already-absolute path. Not patched —
it is the stack's defect, and per the issue's constraints it is documented, not
worked around. Practical consequence: **pass output paths relative to the working
directory**, which is what the CLI's `-out`/`-err` now do.

## 4. Stack defect: the default output path reported does not exist

Submitting without `standardOutput`, `status` reports `/home/demo/slurm-13.out`, and
downloading it fails:

```
$ firecrest-go download /home/demo/slurm-13.out /tmp/final.out
error: FirecREST API error: status=404 detail="{\"errorType\":\"error\",
  \"message\":\"Remote process failed with exit status:1 and error message:
  base64: /home/demo/slurm-13.out: No such file or directory\", …}"
```

The client is reporting faithfully what the server told it. The two halves of the
`fakecluster` stand-in disagree about where default output goes:

```
sbatch  : [ -n "$OUT" ] || OUT_ABS="$JOBDIR/stdout"      # /var/spool/fakeslurm/13/stdout
scontrol: [ -n "$out" ] || out="slurm-${jid}.out"        # reports /home/demo/slurm-13.out
```

`sbatch` writes the job's stdout into its spool directory; `scontrol`, which is what
FirecREST reads the metadata from, reports the conventional Slurm name. The file at
the reported path is never created. This is a defect of the demo stand-in and would
not occur against real Slurm, where the default output genuinely lands in the working
directory — so it should not be read as a v2 API defect.

With an explicit output path the whole chain works:

```
$ firecrest-go submit -name vdlp50 -out vdlp50.out -err vdlp50.err job.sh /home/demo
14
$ firecrest-go status 14
job=14 name=vdlp50 state=COMPLETED exit=0 terminal=true stdout=/home/demo/vdlp50.out stderr=/home/demo/vdlp50.err
$ firecrest-go download /home/demo/vdlp50.out /tmp/final.out
OK (23 bytes): VDLP-50 final run\ndone
```

## 5. Confirmed correct, no change needed

- **Authentication.** `clientcredentials.Config` against the launcher's `/token`
  endpoint yields a usable token; `/status/systems` returns 200 with it and 401
  without. The issue flagged the issuer as a likely first discovery — it is not one,
  the flow works as written.
- **`ops/download` is a byte stream**, `content-type: application/octet-stream`, not
  a JSON envelope. The client's raw-body handling is right. It is *not* `ops/view`,
  so the documented `/ops/view` size-default defect does not apply to this client.
- **`ops/upload`** with multipart field `file` and the directory in `?path=`
  returns 204, matching the client's implementation.
- **The `max_ops_file_size` limit is real and enforced**: downloading a 2 MiB file
  against the configured 1 MiB limit returns `413 {"message":"Command output exceeded
  buffer limit."}`. `Download` surfaces it as an `APIError` with the status intact.
  No streaming/chunked path exists in this client — large files need the async
  `filesystem/{system}/transfer/*` endpoints, still out of scope.
- **`jobId` is returned as a JSON string** (`{"jobId":"14"}`), not the number the
  original unit test assumed. The client already accepted both; a test now pins both.

## Not verified

No real Alps system, no real CSCS credentials. `fakecluster`'s scheduler is a shell
script that runs the job inline and synchronously — there is no queue, so no job was
ever observed in `PENDING` or `RUNNING`, and the terminal-state logic has been
exercised only against `COMPLETED`. Demo tokens are long-lived; refresh behaviour
under a realistic ~300 s TTL is untested.

One further gap, unrelated to this client: `GET /status/{system}/userinfo` fails on
this stack with `sacctmgr: command not found` — the stand-in does not provide that
binary. The endpoint is not used here.
