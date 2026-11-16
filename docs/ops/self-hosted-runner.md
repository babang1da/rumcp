# Self-hosted CI runner

CI for this repository runs on a self-hosted GitHub Actions runner. GitHub-hosted
minutes are not paid for (ZHC-411), so there is no `ubuntu-latest` fallback: if
the runner is missing, every pull request sits in `queued` with no job started.

## Where it lives

| | |
|---|---|
| GitHub | `babang1da/rumcp`, repo-scoped runner `rumcp-dokploy` |
| Host | the Dokploy server, compose project `rumcp` / service `gh-runner-rumcp` |
| Image | `myoung34/github-runner:latest` (runner 2.338.x) |
| Labels | `self-hosted`, `mcp_server`, `rumcp` (+ the automatic `Linux`, `X64`) |

The runner is per-repository: a runner registered in another repository of the
holding never picks up jobs from here. `mcp_server` is kept because workflows
were written with it; `rumcp` marks the runner inside this repository.

## Compose service

The live copy is the `gh-runner-rumcp` compose service in the Dokploy panel.
Keep this file and the panel in sync.

```yaml
services:
  runner:
    image: myoung34/github-runner:latest
    restart: unless-stopped
    # Caps keep a CI build from starving the product stack on this host.
    mem_limit: 2g
    cpus: 1.0
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
      - gh-runner-config-v2:/runner-config

volumes:
  gh-runner-work:
  gh-runner-config-v2:
```

## First registration

`RUNNER_TOKEN` is used exactly once, on the first start with an empty config
volume. Mint a fresh one (valid 1 hour, single use) and put it in the compose
service environment, then deploy:

```bash
gh api -X POST repos/babang1da/rumcp/actions/runners/registration-token --jq .token
```

## Recreating with an expired token

Safe by design: while the config volume holds `.runner`, the entrypoint skips
registration and starts the runner from the stored credentials, so the token
value is irrelevant. Verified on 2026-11-16 — the service was deployed with a
deliberately invalid `RUNNER_TOKEN` and the runner came back `online` under the
same runner id.

If the config volume is ever lost, the token must be minted again.

## Changing labels

Labels are written by `config.sh` only on registration. Editing `LABELS` alone
changes nothing on a container that already has `.runner`. To apply a new label
set, point the service at a fresh config volume (empty directory), mint a fresh
registration token, and deploy: `config.sh --replace` swaps the registration for
the same runner name, keeping the old one's id where GitHub allows it.

## Verification

```bash
gh api repos/babang1da/rumcp/actions/runners \
  --jq '.runners[] | "\(.name) \(.status) \(.busy) \([.labels[].name]|join(","))"'
gh run list --repo babang1da/rumcp --limit 5
```

Expected: one runner, `online`, labels including `self-hosted` and `mcp_server`.
A run stuck in `queued` with an empty `runner_name` means no runner matches the
job's `runs-on` labels.

## A job that never starts

1. `total_count: 0` in the runners API — the container is gone or unregistered;
   deploy the compose service, then re-check.
2. Runner `online` but the job stays `queued` — the job's labels are not a subset
   of the runner's labels. Fix the workflow or add the label (see above).
3. Runner `offline` in the panel or missing there — read the compose logs in
   Dokploy; a `config.sh` failure is usually an expired or reused
   `RUNNER_TOKEN` with a missing config volume.

## Incident log

- **2026-11-16 (ZHC-887):** the repository had no runner at all; PRs #1 and #2
  queued for 20–40 minutes. The runner was created on the Dokploy host from the
  pattern of `gh-runner-natal-ai` (ZHC-520 durability included). Labels were set
  to `self-hosted,mcp_server,rumcp` because PR #2's job asked for `mcp_server`
  while the runner initially registered only `rumcp`.
