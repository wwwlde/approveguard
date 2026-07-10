# 🚪🔒🗝️ ApproveGuard

> ⭐️⭐️⭐️ If you find this project useful, please consider giving it a star on GitHub. This helps draw attention to the project and encourages ongoing development. Thank you!

## Overview

ApproveGuard gates merges on **GitLab Free / Community Edition** by counting merge request
**approvals** — filling the gap with the Premium "required approvals" feature. It has two modes:

1. **`serve`** — a small webhook server that **blocks the Merge button**: it listens for GitLab
   merge request webhooks and posts a commit status (`approve-guard`) of `failed`/`success`
   depending on whether the MR has enough approvals. Combined with the *"Pipelines must succeed"*
   merge check (available in Free), GitLab itself keeps the Merge button disabled until the
   threshold is met — and re-enables it the moment the last approval arrives.
2. **`check`** — the classic one-shot CLI gate for CI workflows (e.g. an
   [Atlantis](https://www.runatlantis.io/) `apply` step): exits `0` if the MR has enough
   approvals, `1` otherwise. The legacy *upvotes* (👍) metric is still available via
   `--mode upvotes`.

### Why?

GitLab Free allows any user with the Developer role to approve merge requests, and the
approvals API (`GET .../approvals`, `approved_by`) is available on all tiers — but Free cannot
*require* approvals or block the merge. ApproveGuard adds that enforcement externally, using
only APIs and settings available in the Free tier.

## 🌟 Features

- **Merge button blocking** on GitLab Free via commit statuses + "Pipelines must succeed"
- Counts real **approvals** (who approved is logged by username), not emoji reactions
- The MR author's self-approval is **excluded by default** (`--count-author-approval` to allow)
- **Approvals reset on new commits** (like the Premium setting; on by default in `serve`,
  requires a project/group access token, i.e. a bot user; disable with
  `--reset-approvals-on-push=false`)
- Per-project threshold override right in the webhook URL: `/webhook?approvals=3`
- Legacy upvotes mode for backwards compatibility (`check --mode upvotes`)
- Single static binary, distroless container image

## Requirements

- Go 1.24+ (build)
- GitLab token:
  - `check`: `read_api` scope is enough
  - `serve`: **`api` scope** (posts commit statuses); to also reset approvals on push it must
    be a **project or group access token** (bot user) with the Developer+ role

## 🚀 Installation

To build the project, run:

```
CGO_ENABLED=0 go build -o ./ApproveGuard --ldflags '-w -s -extldflags "-static"' .
```

To run the tests:

```
go test ./...
```

## ▶️ Webhook server (`serve`)

```bash
$ export GITLAB_TOKEN=glpat-XXX            # api scope
$ export GITLAB_WEBHOOK_SECRET=change-me
$ export APPROVALS=2                        # default threshold
$ ./ApproveGuard serve
2026/07/10 12:00:00 ApproveGuard v1.0.0 listening on :8080 (default threshold: 2 approvals)
```

| Flag | Env var | Default | Description |
|---|---|---|---|
| `--listen` | `LISTEN_ADDR` | `:8080` | Address to listen on |
| `--webhook-secret` | `GITLAB_WEBHOOK_SECRET` | — (required) | Webhook secret token |
| `--token` | `GITLAB_TOKEN` | — (required) | GitLab token, `api` scope |
| `--gitlab-url` | `GITLAB_URL` | derived from payload | GitLab base URL |
| `--approvals` | `APPROVALS` | `1` | Default approvals threshold |
| `--count-author-approval` | `COUNT_AUTHOR_APPROVAL` | `false` | Count the author's own approval |
| `--reset-approvals-on-push` | `RESET_APPROVALS_ON_PUSH` | `true` | Reset approvals on new commits |

Endpoints: `POST /webhook` (GitLab webhook receiver, optional `?approvals=N` per-project
threshold override), `GET /healthz`.

### GitLab project setup (once per project)

1. Create a **project access token** (Settings → Access tokens) with role *Developer* and scope
   `api` — this creates a bot user; use the token as `GITLAB_TOKEN`.
2. Enable **Settings → Merge requests → Merge checks → "Pipelines must succeed"**.
3. Add a webhook (Settings → Webhooks): URL `https://approveguard.example.com/webhook`
   (append `?approvals=3` to override the threshold for this project), your secret token, and
   trigger **Merge request events**.

That's it: every MR now shows an `approve-guard` status in the pipeline widget
(`1/2 approvals`), and the Merge button stays disabled until the status turns green.

## ▶️ One-shot gate (`check`)

```bash
$ export GITLAB_TOKEN=glpat-XXX
$ export PULL_NUM=1
$ export PULL_URL=https://gitlab.example.com/devops/terraform/project/-/merge_requests/1
$ ./ApproveGuard check --approvals 2
2026/07/10 12:00:00 Numeric Project ID: 91
2026/07/10 12:00:00 Merge request Author: Denys Lemeshko (denys.lemeshko)
2026/07/10 12:00:00 Approved by: alice, bob
2026/07/10 12:00:00 Merge request has sufficient approvals (2/2 approvals).
$ echo $?
0
```

| Flag | Env var | Default | Description |
|---|---|---|---|
| `--token` | `GITLAB_TOKEN` | — (required) | GitLab token, `read_api` is enough |
| `--url` | `PULL_URL` | — (required) | Merge request URL |
| `--pull-num` | `PULL_NUM` | — (required) | Merge request IID |
| `--mode` | `CHECK_MODE` | `approvals` | `approvals` or `upvotes` (legacy) |
| `--approvals` | `APPROVALS` | `1` | Threshold |
| `--upvotes` | `UPVOTES` | — | Legacy threshold alias (used when `--approvals` unset) |
| `--count-author-approval` | `COUNT_AUTHOR_APPROVAL` | `false` | Count the author's own approval |

![Atlantis](./atlantis.png)

---

## Atlantis configuration example

`docker-compose.yaml`

```yaml
version: "3.8"

networks:
  traefik-public:
    external: true

services:
  atlantis:
    container_name: atlantis
    image: registry.example.com/docker/atlantis:v0.0.2-d97e6784
    restart: unless-stopped
    expose:
      - "4141"
    command:
      - 'server'
      - '--atlantis-url=https://atlantis.example.com'
      - '--gitlab-webhook-secret=XXX'
      - '--gitlab-user=git'
      - '--repo-allowlist=gitlab.example.com/devops/terraform/*'
      - '--repo-config=/data/repos.yaml'
      - '--write-git-creds'
    volumes:
      - ./repos.yaml:/data/repos.yaml
    networks:
      - traefik-public
    environment:
      - "APPROVALS=2"
      - "ATLANTIS_GITLAB_HOSTNAME=gitlab.example.com"
      - "ATLANTIS_GITLAB_TOKEN=glpat-XXX"
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.atlantis-web-secure.rule=Host(`atlantis.example.com`)"
      - "traefik.http.routers.atlantis-web-secure.tls=true"
      - "traefik.http.routers.atlantis-web-secure.tls.certresolver=myresolver"
      - "traefik.http.routers.atlantis-web-secure.entrypoints=websecure"
      - "traefik.http.routers.atlantis-web-secure.service=atlantis-web-secure"
      - "traefik.http.services.atlantis-web-secure.loadbalancer.server.port=4141"

  approveguard:
    container_name: approveguard
    image: ghcr.io/wwwlde/approveguard:v1.0.0
    restart: unless-stopped
    command: ['serve']
    expose:
      - "8080"
    networks:
      - traefik-public
    environment:
      - "GITLAB_TOKEN=glpat-XXX"                # project/group access token, api scope
      - "GITLAB_WEBHOOK_SECRET=change-me"
      - "APPROVALS=2"
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.approveguard.rule=Host(`approveguard.example.com`)"
      - "traefik.http.routers.approveguard.tls=true"
      - "traefik.http.routers.approveguard.tls.certresolver=myresolver"
      - "traefik.http.routers.approveguard.entrypoints=websecure"
      - "traefik.http.services.approveguard.loadbalancer.server.port=8080"
```

`repos.yaml`

```yaml
repos:
  - id: /.*/
    apply_requirements:
      - approved
      - mergeable
    workflow: terragrunt
workflows:
  terragrunt:
    plan:
      steps:
      - run: echo "Run \"terraform fmt\" command" && terraform fmt -check -diff
      - env:
          name: TERRAGRUNT_TFPATH
          command: "echo terraform$ATLANTIS_TERRAFORM_VERSION"
      - run: bash -o pipefail -c "terragrunt plan -no-color -out=${PLANFILE}"
    apply:
      steps:
      - run: ApproveGuard check --token $ATLANTIS_GITLAB_TOKEN --pull-num $PULL_NUM --approvals 2 --url https://${ATLANTIS_GITLAB_HOSTNAME}/${BASE_REPO_OWNER}/${BASE_REPO_NAME}/-/merge_requests/${PULL_NUM}
      - env:
          name: TERRAGRUNT_TFPATH
          command: 'echo terraform$ATLANTIS_TERRAFORM_VERSION'
      - run: bash -o pipefail -c "terragrunt apply -no-color ${PLANFILE}"
```

## Upgrading from v0.x

- The root command became the `check` subcommand: `ApproveGuard --upvotes 2 ...` →
  `ApproveGuard check --upvotes 2 --mode upvotes ...` (or drop `--mode` to switch to approvals).
- `check` counts **approvals** by default; pass `--mode upvotes` to keep the old behaviour.
- The MR author's own approval is not counted unless `--count-author-approval` is set.

## 🤝 Contributing

Contributions to this project are welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) for details.

## 📜 License

This project is licensed under the GNU General Public License v3.0 (GPL-3.0). See the [LICENSE](LICENSE) file for details.
