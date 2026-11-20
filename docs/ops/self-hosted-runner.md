# Self-hosted CI runner

CI for this repository runs on a self-hosted GitHub Actions runner. GitHub-hosted
minutes are not paid for (ZHC-411), so there is no `ubuntu-latest` fallback: if
the runner is missing, every pull request sits in `queued` with no job started.

## Where it lives

| | |
|---|---|
| GitHub | `babang1da/rumcp`, repo-scoped runner `rumcp-dokploy` (the numeric id changes on every re-registration; match by name) |
| Host | the Dokploy server, compose project `rumcp` / service `gh-runner-rumcp` |
| Image | `myoung34/github-runner:latest` (runner 2.338.x) |
| Labels | `self-hosted`, `mcp_server`, `rumcp` (+ the automatic `Linux`, `X64`) |

The runner is per-repository: a runner registered in another repository of the
holding never picks up jobs from here. `mcp_server` marks the MCP products of the
holding and is kept for workflows written with it; `rumcp` marks this repository.
Jobs in `.github/workflows/ci.yml` ask for `self-hosted` only, so on today's
`develop` the extra labels are descriptive, not routing.

One runner process takes one job at a time. A run with two jobs therefore keeps
its run-level status `queued` while its second job waits: check the jobs, not the
run (`gh api repos/babang1da/rumcp/actions/runs/<id>/jobs`).

## Compose service

The live copy is the `gh-runner-rumcp` compose service in the Dokploy panel
(`compose.one` for `JycKHBFhaWMDDDDmM9LPm`). Keep this file and the panel in sync.

```yaml
services:
  runner:
    image: myoung34/github-runner:latest
    restart: unless-stopped
    # Caps keep a CI build from starving the product stack on this host.
    mem_limit: 2g
    cpus: 0.5
    pids_limit: 512
    environment:
      REPO_URL: https://github.com/babang1da/rumcp
      RUNNER_SCOPE: repo
      RUNNER_NAME: rumcp-dokploy
      RUNNER_TOKEN: ${RUNNER_TOKEN}
      LABELS: self-hosted,mcp_server,rumcp
      # EPHEMERAL must stay unset: the image treats ANY non-empty value as
      # enabled and exits the runner after one job.
      DISABLE_AUTO_UPDATE: "true"
      RUNNER_WORKDIR: /home/runner/_work
      # Keep the registration (.runner/.credentials) between container
      # recreations. Without the config volume the entrypoint deletes it and
      # re-registers on every start, which needs a live registration token
      # (1 h TTL, single use); after it expired a restart becomes a crash loop
      # and the runner vanishes from GitHub while jobs stay queued silently.
      # The image refuses to start with a config dir unless automatic
      # deregistration is off, so both settings go together.
      CONFIGURED_ACTIONS_RUNNER_FILES_DIR: /runner-config
      DISABLE_AUTOMATIC_DEREGISTRATION: "true"
      RUNNER_GROUP: Default
    volumes:
      - gh-runner-work:/home/runner/_work
      - gh-runner-config-v3:/runner-config

volumes:
  gh-runner-work:
  gh-runner-config2:
  # v3 = clean registration, labels self-hosted,mcp_server,rumcp (ZHC-887).
  gh-runner-config-v3:
```

`${RUNNER_TOKEN}` is a compose-time variable: its value is the `RUNNER_TOKEN`
line in the service **Environment** field in the Dokploy panel, not a file in
this repository and not `.env` on the host. The panel substitutes it when the
service is deployed.

## First registration

`RUNNER_TOKEN` is used exactly once, on the first start with an empty config
volume. Mint a fresh one (valid 1 hour, single use), put it in the panel's
Environment field, and deploy:

```bash
gh api -X POST repos/babang1da/rumcp/actions/runners/registration-token --jq .token
```

The panel keeps the value after it has been spent; a later start with a
non-empty config volume ignores it.

## Recreating with an expired token

The registration survives a redeploy: while the config volume holds `.runner`,
the entrypoint logs `The runner has already been configured`, never calls
`config.sh`, and the token value is irrelevant — spent, expired or invalid
alike. Verified on 2026-11-16 21:24 UTC, with the panel token spent an hour
before: the container was recreated, the runner kept its id, and it picked a
job again (see the conflict note below). If the config volume is ever lost, the
token must be minted again.

**The first ~2 minutes after a recreation are a session conflict, not a
failure.** A container that is stopped without deregistering leaves its listener
session on GitHub, so the new process loops:

```
A session for this runner already exists.
Runner connect error: Error: Conflict. Retrying until reconnected.
```

The loop recovers on its own once GitHub drops the old session — measured
21:24:50 → `Runner reconnected` + `Listening for Jobs` 21:26:23, first job
13 s later. Jobs queued in that window are not lost. Do not redeploy again
while the loop runs: that restarts the clock. `online` in the runners API does
not prove the listener works — only `Listening for Jobs` in the log does.

If the loop runs longer than ~5 minutes, treat it as a real stall: delete the
stale runner entry (`DELETE /repos/babang1da/rumcp/actions/runners/<id>`) and
deploy with a fresh config volume and a fresh registration token.

## Changing labels

Labels are written by `config.sh` only on registration. Editing `LABELS` alone
changes nothing on a container that already has `.runner`. To apply a new label
set, point the service at a fresh config volume (empty directory), mint a fresh
registration token, and deploy: `config.sh --replace` swaps the registration for
the same runner name, keeping the old one's id where GitHub allows it. This is
how `mcp_server` was added on 2026-11-16 (`gh-runner-config-v2` →
`gh-runner-config-v3`).

## Verification

```bash
gh api repos/babang1da/rumcp/actions/runners \
  --jq '.runners[] | "\(.name) \(.status) \(.busy) \([.labels[].name]|join(","))"'
gh run list --repo babang1da/rumcp --limit 5
gh api repos/babang1da/rumcp/actions/runs/<id>/jobs --jq '.jobs[]|"\(.name): \(.status) \(.runner_name)"'
```

Expected: one runner, `online`, labels `self-hosted,Linux,X64,rumcp,mcp_server`.
A job stuck in `queued` with an empty `runner_name` either matches no runner
(labels) or is not being polled (next section). A run-level `queued` with one
job `in_progress` is normal on a single runner.

## A job that never starts

1. `total_count: 0` in the runners API — the container is gone or unregistered;
   deploy the compose service, then re-check.
2. Runner `online`, the job is `queued` with an empty `runner_name` and its
   labels are a subset of the runner's — check `busy` first: `busy: true` means
   the runner is working another job and this one is next in line. When the
   runner is idle, read the container log in Dokploy (`compose.readLogs`): a
   `Conflict … Retrying until reconnected` loop is a leftover session from a
   recreation and clears in ~2 minutes by itself (see above); a container that
   never printed `Listening for Jobs` after its last start has no usable
   `.runner` and needs a fresh config volume with a fresh registration token.
3. Runner `offline` in the panel or missing there — read the compose logs in
   Dokploy; a `config.sh` failure is usually an expired or reused
   `RUNNER_TOKEN` with a missing config volume.

## Incident log

- **2026-11-16 (ZHC-887):** the repository had no runner at all; PRs #1 and #2
  queued for 20–40 minutes. The runner was created on the Dokploy host from the
  pattern of `gh-runner-natal-ai` (ZHC-520 durability included), first with the
  `rumcp` label only. Between 21:06 and 21:18 UTC queued jobs sat on an `online`
  runner that was not listening — the session-conflict loop above, left by a
  recreation test. A clean registration on `gh-runner-config-v3` with
  `mcp_server` added restored job pickup at 21:18:26. The same redeploy at 21:24
  (spent token, kept registration) reproduced the conflict and recovered by
  itself in 1.5 minutes, which is how the timing above was measured.
