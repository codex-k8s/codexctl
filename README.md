<div align="center">
  <img src="docs/media/logo.png" alt="PAI logo" width="120" height="120" />
  <h1>codexctl</h1>
  <p>🧠 A tool for managing cloud planning and development workflows in a Kubernetes cluster through AI agents based on <a href="https://github.com/openai/codex">OpenAI's codex-cli</a> and GitHub workflows.</p>
  <p>🚧 Alpha version. Breaking changes are possible.</p>
</div>

![Go Version](https://img.shields.io/github/go-mod/go-version/codex-k8s/codexctl)
[![Go Reference](https://pkg.go.dev/badge/github.com/codex-k8s/codexctl.svg)](https://pkg.go.dev/github.com/codex-k8s/codexctl)

`codexctl` is a CLI tool for declarative management of Kubernetes environments and dev-AI slots using a single
`services.yaml` configuration file. It simplifies:

- deploying infrastructure (DBs, caches, ingress, observability) and applications in Kubernetes projects;
- preparing temporary dev-AI environments for issues/PRs where a Codex agent works;
- rendering manifests and configs (including the Codex config) using templates.

Essentially, it is an "orchestrator on top of `kubectl` and templates" that is aware of:

- environments (`dev`, `staging`, `ai`);
- slots (`ai` environments for issues/PRs);
- project structure (infrastructure, services, the Codex agent Pod).

> Important: the tool is in an early stage of development; see the "Security and stability" section at the end.

---

## 📦 Installation

`codexctl` is distributed as a Go CLI. With a Go toolchain installed you can install it via:

```bash
go install github.com/codex-k8s/codexctl/cmd/codexctl@latest
```

Or a specific version (replace `v0.1.0` with the desired SemVer tag):

```bash
go install github.com/codex-k8s/codexctl/cmd/codexctl@v0.1.0
```

You can also browse the Go reference on pkg.go.dev: https://pkg.go.dev/github.com/codex-k8s/codexctl.

---

## 💡 1. Core ideas

### 📦 1.1. Single `services.yaml` for the whole project

Instead of scattered Helm charts and bash scripts, you use a single `services.yaml` file that defines:

- which images to use and how to build them (`images`);
- which infrastructure manifests to apply (`infrastructure`);
- which services to deploy (`services`);
- what environments look like (`environments`), namespaces and slots (`namespace`, `state`);
- how the Codex agent Pod is configured (`codex`).

This file is the single source of truth for `codexctl`, GitHub Actions and dev-AI environments.

### 🧩 1.2. Templating and context

`services.yaml` and all referenced manifests are rendered using Go templates. In templates, you have access to:

- `{{ .Env }}` — current environment (`dev`, `staging`, `ai`, `ai-repair`);
- `{{ .Namespace }}` — Kubernetes namespace;
- `{{ .Project }}` — project name (`codex-project`);
- `{{ .Slot }}` — slot number for a dev-AI environment;
- `{{ .BaseDomain }}` — map of base domains (`dev`, `staging`, `ai`, `ai-repair`);
- `{{ .Versions }}` — map of service/image versions;
- helper functions like `envOr`, `default`, `ternary`, `join`, etc.

The same context is used by:

- manifest rendering (`codexctl apply` / `codexctl ci apply`);
- built-in prompt templates (`internal/prompt/templates/*.tmpl`);
- the Codex config template (`internal/prompt/templates/config_default.toml` or an overridden one).

### 🌐 1.3. Environments and slots

`codexctl` works with the following environment types:

- `dev` — local developer environment (a single namespace);
- `staging` — staging cluster (CI/CD, close to production);
- `ai` — dev-AI slots: isolated namespaces of the form `<project>-dev-<slot>` (for example, `codex-project-dev-<slot>`),
  with domains like `dev-<slot>.staging.<domain>` where Codex agents work on issues/PRs.
- `ai-repair` — a dedicated namespace with a Codex Pod and RBAC access to staging (for repairs).

Slots (`slot`) are numeric identifiers of dev-AI environments managed by `codexctl ci ensure-slot/ensure-ready`. For each
slot `codexctl` creates and maintains:

- a dedicated namespace;
- a dedicated set of PVCs/data (`.data/slots/<slot>` on the host);
- a dedicated `codex` Pod with the agent image and your project sources mounted (in the examples — `codex-project`).

### 🧪 1.4. Issue flow and the agent's role

The basic idea is:

- you create an Issue in the repository and add a label to it, for example `[ai-plan]` for planning
  or `[ai-dev]` for development;
- a GitHub Actions workflow reacts to this label, calls `codexctl ci ensure-slot/ensure-ready --env ai ...`
  and deploys a full stack of project infrastructure and services into a separate namespace;
- in this namespace a `codex` Pod with a Codex agent is started, and `codexctl prompt run` feeds it a prompt
  of the required type (`kind=plan_issue` or `kind=dev_issue`, languages `ru`/`en`).

The key property of this approach is that the agent works **in a live environment** and "debugs" its changes the same way
a developer would:

- reads service logs via `kubectl logs`;
- talks to DBs and caches (via `psql`, `redis-cli` or your own CLI/HTTP/gRPC clients);
- sends real requests to the project's HTTP/gRPC endpoints;
- can run tests, migrations, load fixtures, restart deployments.

Each dev-AI environment is isolated (its own namespace and data), so the agent does not interfere with other developers
and does not touch production services.

---

## 🚀 2. Getting started

### ✅ 2.1. Requirements

- A Kubernetes cluster (separate from production).
- Accessible `kubectl` and kubeconfig for the chosen environment.
- Docker daemon and (recommended) a local image registry.
- Built `codexctl` binary available in `PATH`.

### 📝 2.2. Minimal `services.yaml` for a project

A minimal example:

```yaml
project: codex-project

baseDomain:
  dev: "dev.codex-project.com"
  staging: "staging.codex-project.com"
  ai: "staging.codex-project.com"

environments:
  dev:
    kubeconfig: "/home/user/.kube/codex-project-dev"
    imagePullPolicy: IfNotPresent
  staging:
    kubeconfig: "/home/runner/.kube/codex-project-staging"
    imagePullPolicy: Always
  ai:
    from: "staging"
    imagePullPolicy: IfNotPresent

images:
  app-backend:
    type: build
    repository: "registry.local/codex-project/app-backend"
    dockerfile: "services/app_backend/Dockerfile"
    context: "services/app_backend"

infrastructure:
  - name: base
    manifests:
      - path: deploy/namespace.yaml
      - path: deploy/postgres.yaml
      - path: deploy/redis.yaml

services:
  - name: app-backend
    manifests:
      - path: services/app_backend/deploy.yaml
```

In a real project these blocks will be richer (versions, hooks, overlays), but the basic idea is the same.

### 🔁 2.3. Basic deployment loop

For any environment (`dev`, `staging`, `ai`) the deployment loop is the same:

```bash
ENV=staging   # or dev/ai

codexctl images mirror --env "$ENV"    # if needed
codexctl images build  --env "$ENV"    # build and push images with images.type=build
codexctl apply        --env "$ENV" --wait --preflight
```

When running via GitHub Actions this loop is embedded into the workflow — see the integration section.

---

## 📑 3. `services.yaml` format

`services.yaml` is the "manifest of manifests" for your project. Below is an overview of the key blocks.

### 🌱 3.1. Root fields

- `project` — project code, used in namespaces and other templates.
- `envFiles` — list of `.env` files with environment variables included during rendering.
- `registry` — base registry address (for example, `registry.local:32000`).
- `versions` — a dictionary of versions (arbitrary keys used in templates).

### 🤖 3.2. `codex` block

Configuration of the Codex agent integration:

- `codex.configTemplate` — path to the Codex config template (for example, `deploy/codex/config.toml`). If omitted,
  the built-in `internal/prompt/templates/config_default.toml` is used.
- `codex.links` — list of links (title + path) rendered in environment comments (for example, Swagger, Admin).
- `codex.extraTools` — list of additional CLI tools available in the agent image and useful in prompts
  (for example, `psql`, `redis-cli`, `k6`).
- `codex.projectContext` — free-form text with project specifics (where to find docs, how to run tests, etc.);
  injected into prompts (see built-in templates).
- `codex.servicesOverview` — overview of infrastructure/application services and their URLs/ports; also included in prompts.
- `codex.timeouts.exec`/`codex.timeouts.rollout` — timeouts for `prompt run` and waiting for rollouts.

These fields are used when rendering built-in prompts (`dev_issue_*`, `plan_issue_*`, `plan_review_*`,
`dev_review_*`, `ai-repair_*`) and the Codex config:

- `internal/prompt/templates/*.tmpl` — prompt templates;
- `internal/prompt/templates/config_default.toml` — default Codex config.

You can override:

- the Codex config via `codex.configTemplate`;
- the prompts themselves — by passing your own `--template` to `codexctl prompt ...` or replacing the built-in `.tmpl`
  files in the agent image.

### 🌐 3.3. `baseDomain` and `namespace`

```yaml
baseDomain:
  dev: "dev.codex-project.local"
  staging: "staging.codex-project.local"
  ai: "staging.codex-project.local"

namespace:
  patterns:
    dev: "{{ .Project }}-dev"
    staging: "{{ .Project }}-staging"
    ai: "{{ .Project }}-dev-{{ .Slot }}"
```

- `baseDomain` — ingress domains per environment.
- `namespace.patterns` — namespace templates; for `ai` the default is `project-dev-<slot>`.

### 🗺️ 3.4. `environments`

Cluster connection settings:

```yaml
environments:
  dev:
    kubeconfig: "/home/user/.kube/codex-project-dev"
    imagePullPolicy: IfNotPresent
    localRegistry:
      enabled: true
      name: "codex-project-registry"
      port: 32000
  staging:
    kubeconfig: "/home/runner/.kube/codex-project-staging"
    imagePullPolicy: Always
  ai:
    from: "staging"
    imagePullPolicy: IfNotPresent
```

- `from` lets you inherit settings (for example, `ai` from `staging`).
- `localRegistry` describes a local registry where images are pushed by `images build`.

### 🖼️ 3.5. `images`

Describes external and built images:

```yaml
images:
  busybox:
    type: external
    from: "docker.io/library/busybox:1.37.0"
    local: "{{ envOr \"REGISTRY_HOST\" \"registry.local:32000\" }}/library/busybox:1.37.0"

  app-backend:
    type: build
    repository: "{{ envOr \"REGISTRY_HOST\" \"registry.local:32000\" }}/codex-project/app-backend"
    dockerfile: "services/app_backend/Dockerfile"
    context: "services/app_backend"
    buildArgs:
      SERVICE_VERSION: "{{ index .Versions \"app-backend\" }}"
```

- `type: external` — images mirrored by `images mirror`;
- `type: build` — images built and pushed by `images build`.

### 🏗️ 3.6. `infrastructure`

List of infrastructure "packages":

```yaml
infrastructure:
  - name: base
    manifests:
      - path: deploy/namespace.yaml
      - path: deploy/postgres.yaml
      - path: deploy/redis.yaml
    hooks:
      afterApply:
        - name: wait-postgres
          use: kubectl.wait
          with:
            kind: Deployment
            name: postgres
            namespace: "{{ .Namespace }}"
            condition: Available
            timeout: "1200s"
```

Each item:

- describes a set of YAML files (with templates);
- may contain `hooks.beforeApply/afterApply/afterDestroy` with `kubectl` calls or shell scripts.

### 🧱 3.7. `services`

List of applications:

```yaml
services:
  - name: app-backend
    manifests:
      - path: services/app_backend/deploy.yaml
    image:
      repository: "{{ envOr \"REGISTRY_HOST\" \"registry.local:32000\" }}/codex-project/app-backend"
      tagTemplate: "{{ printf \"%s-%s\" .Env (index .Versions \"app-backend\") }}"
    overlays:
      dev:
        hostMounts:
          - name: go-src
            hostPath: "{{ .ProjectRoot }}/services/app_backend"
            mountPath: "/app"
      ai:
        hostMounts:
          - name: go-src
            hostPath: "{{ printf \"%s/%d/src/services/app_backend\" (envOr \"CODE_ROOT_BASE\" \"/srv/repo\") .Slot }}"
            mountPath: "/app"
        dropKinds: ["Ingress"]
```

- `manifests` — list of YAML files for the service;
- `image` — image override in manifests (repository/tag);
- `overlays` — environment-specific settings (hostPath mounts for sources, disabling ingress in dev-AI, etc.);
- `hostMounts` — list of host directories to mount (local sources for dev/dev-AI).
  Optional: `hostPathType` (defaults to `Directory`). Use `Socket` for mounts like `/var/run/docker.sock`.
- `dropKinds` — list of Kubernetes resource kinds to drop from rendering (e.g., Ingress in dev-AI).

---

## 🛠️ 4. Applying manifests

### ☸️ 4.1. `codexctl apply`

```bash
codexctl apply \
  --env staging \
  --slot 0 \
  --wait \
  --preflight
```

This command:

- renders the stack;
- runs preflight checks (if the `--preflight` flag is set);
- applies manifests via `kubectl apply`;
- runs `afterApply` hooks (for example, waiting for rollouts);
- with `--wait` waits for deployments to become ready.

Filtering flags (safe apply):

- `--only-services name1,name2` — apply only selected services.
- `--skip-services name1,name2` — skip selected services.
- `--only-infra name1,name2` — apply only selected infra blocks.
- `--skip-infra name1,name2` — skip selected infra blocks.

When running inside the Codex Pod, always use filters (for example `--only-services`
or `--only-infra`) and avoid applying the `codex` service itself.

### 🧩 4.2. `codexctl render`

Render manifests without applying them:

```bash
codexctl render \
  --env staging \
  --only-services web-frontend
```

---

## ⌨️ 5. `codexctl` commands: overview

### ⚙️ 5.1. Global flags

- `--config, -c` — path to `services.yaml` (defaults to `services.yaml` in the current directory).
- `--env` — environment name (`dev`, `staging`, `ai`).
- `--namespace` — explicit namespace override (usually not needed).
- `--log-level` — log level (`debug`, `info`, `warn`, `error`).

### ☸️ 5.2. `apply`

- Purpose: render and apply the stack to Kubernetes.
- Typical example — see section 4.1.

### 🧩 5.3. `render`

- Purpose: render manifests to stdout without applying them.
- Useful in CI or inside Codex pods to inspect what would be applied.

### 🧪 5.4. `ci`

Helpers for CI workflows and slot provisioning.

Subcommands:

- `ci images` — mirrors external images and/or builds local ones for CI.
  Flags: `--mirror/--build` (both default to `true`), `--slot`, `--vars`, `--var-file`.
- `ci apply` — apply manifests with retries and optional wait.
  Flags: `--preflight`, `--wait`, `--apply-retries`, `--wait-retries`, `--apply-backoff`,
  `--wait-backoff`, `--wait-timeout`, `--request-timeout`, and render filters
  (`--only-services/--skip-services/--only-infra/--skip-infra`).
- `ci ensure-slot` — allocate or reuse a slot by `--issue/--pr/--slot` selector (one is required).
  Output: `plain|json|kv`.
- `ci ensure-ready` — ensure a slot exists and optionally sync sources, prepare images, and apply manifests.
  Flags: `--code-root-base`, `--source`, `--prepare-images`, `--apply`, `--force-apply`,
  `--wait-timeout`, `--wait-soft-fail`, output `plain|json|kv`.
  When `--code-root-base` and `--source` are set, sources are synced to `<code-root-base>/<slot>/src`.

### 🖼️ 5.5. `images`

Subcommands:

- `images mirror` — mirrors `images.type=external` into a local registry:

  ```bash
  codexctl images mirror --env staging
  ```

- `images build` — builds and pushes `images.type=build`:

  ```bash
  codexctl images build --env staging
  ```

### 🎛️ 5.6. `manage-env`

Command group for dev-AI slot metadata and cleanup (`env=ai`):

- `manage-env cleanup` — delete a slot environment and its state records.
- `manage-env set` — set slot ↔ issue/PR mappings.
- `manage-env comment` — render environment links for PR/Issue comments.

Notes:

- `manage-env cleanup` supports `--all` (cleanup all matching slots) and `--with-configmap`
  (remove state ConfigMap for the selected environments).
- `manage-env comment` accepts `--lang en|ru` for the rendered message.

### 🧠 5.7. `prompt`

Commands for working with Codex agent prompts:

- `prompt run` — run a Codex agent in the `codex` Pod:

  ```bash
  codexctl prompt run \
    --env ai \
    --slot 1 \
    --kind dev_issue \
    --lang ru
  ```

  This uses built-in prompt templates (`internal/prompt/templates/dev_issue_*.tmpl`) and the `services.yaml` context
  (`codex.extraTools`, `codex.projectContext`, `codex.servicesOverview`, `codex.links`).

Notes:

- `prompt run` supports `--issue`/`--pr` context, `--resume`, `--infra-unhealthy`, `--vars`, `--var-file`.
- `--template` overrides `--kind`; when `--kind` is not set it defaults to `dev_issue`.

### 🧭 5.8. `plan`

Commands for working with plans and related task structures:

- `plan resolve-root` — find the "root" planning Issue for a specific task:

  ```bash
  codexctl plan resolve-root \
    --issue 123 \
    --repo owner/codex-project \
    --output json
  ```

  This command uses:
  - the `[ai-plan]` label on the root planning Issue;
  - the `AI-PLAN-PARENT: #<root>` marker in child Issues.

This mechanism lets you build a tree of tasks: one planning Issue with `[ai-plan]` describes the architecture and phases,
and child Issues with `AI-PLAN-PARENT: #<root>` are implemented in separate dev-AI slots (`[ai-dev]`) via `ci ensure-ready`
and `prompt run`.

### 🔄 5.9. `pr review-apply`

- Automatically applies changes made by a Codex agent in a dev-AI environment to a PR:

  ```bash
  codexctl pr review-apply \
    --env ai \
    --slot 1 \
    --pr 42 \
    --code-root-base "/srv/codex/envs" \
    --lang ru
  ```

- The command:
  - runs `git add/commit/push` to the PR branch;
  - leaves a comment in the PR with links to the environment.

---

## 🌍 6. Environment variables

`codexctl` uses a merged map of variables:

- process environment variables (`os.Environ()`);
- variables from `envFiles` in `services.yaml`;
- variables from `--var-file` and `--vars`.

Through the `envOr` function these variables are available in templates:

```yaml
registry: "{{ envOr \"REGISTRY_HOST\" \"registry.local:32000\" }}"
```

Commonly used variables:

- `KUBECONFIG` — path to kubeconfig if not set in `environments.*.kubeconfig`;
- `REGISTRY_HOST` — image registry address;
- `CODE_ROOT_BASE` — base path for slot working directories (used in `services.*.overlays.ai.hostMounts`);
- `DATA_ROOT` — base path to `.data` with Postgres/Redis/cache data.

In GitHub Actions you usually set:

- `GITHUB_RUN_ID`, `GITHUB_REPOSITORY`, `DEV_SLOTS_MAX` — to link slots and CI runs;
- secrets for DB/Redis/cache and other external services;
- `CODEX_GH_PAT`, `CODEX_GH_USERNAME` — token and username for the GitHub bot;
- `CONTEXT7_API_KEY` — API key for Context7 (if used);
- `OPENAI_API_KEY` — OpenAI API key.

---

## 🔐 7. GitHub Actions integration and secrets

### 🚀 7.1. Minimal workflow for staging

```yaml
name: "Staging deploy"

on:
  push:
    branches: [ main ]

jobs:
  deploy:
    runs-on: self-hosted
    steps:
      - uses: actions/checkout@v4

      - name: "Build codexctl"
        run: |
          go build -o ./codexctl ./cmd/codexctl

      - name: "Mirror images"
        env:
          REGISTRY_HOST: registry.local:32000
        run: ./codexctl images mirror --env staging

      - name: "Build images"
        env:
          REGISTRY_HOST: registry.local:32000
        run: ./codexctl images build --env staging

      - name: "Apply stack"
        env:
          KUBECONFIG: /path/to/staging.kubeconfig
        run: ./codexctl apply --env staging --wait --preflight
```

### 🤖 7.2. Minimal workflow for a dev-AI slot per Issue

High-level flow:

1. `ci ensure-slot --env ai --issue <N>` — pick/create a slot.
2. `ci ensure-ready --env ai --slot <SLOT> --prepare-images --apply` — deploy infrastructure and services.
3. `prompt run --env ai --slot <SLOT> --kind dev_issue` — start a Codex agent.
4. `manage-env cleanup --env ai --issue <N>` — clean up the slot (if needed).

### 🔑 7.3. Secrets and PAT for the GitHub bot

Recommended set of secrets/vars in your project repository (for example, `codex-project`):

- `CODEX_GH_PAT` — PAT of the GitHub bot user;
- `CODEX_GH_USERNAME` — bot username;
- `KUBECONFIG`/paths to kubeconfig for staging;
- secrets for DB/Redis/cache/queue (username/password, DSN, etc.);
- `REGISTRY_HOST` and (optionally) registry credentials;
- `OPENAI_API_KEY` — OpenAI API key;
- `CONTEXT7_API_KEY` — API key for Context7 (if used).

How to create a user and PAT:

1. Create a separate technical GitHub account for the bot (for example, `codex-bot-42`).
2. In account settings go to **Developer settings → Personal access tokens → Fine-grained**.
3. Create a token with permissions:
   - access to the project repository (for example, `codex-project`, read/write for `code`, `pull requests`, `issues`);
   - access to Actions (if you need to control workflows).
4. Save the token and add it as `CODEX_GH_PAT` in repository secrets.

---

## 🐳 8. Codex agent image (project example)

The example agent Dockerfile lives in the project example repo:
`github.com/codex-k8s/project-example/deploy/codex/Dockerfile`.

It includes everything the agent needs inside the pod:

- Node + Codex CLI (`@openai/codex`);
- Go toolchain + plugins (`protoc-gen-go`, `protoc-gen-go-grpc`, `wire`);
- `protoc` and standard includes;
- Python + virtualenv with basic libraries (`requests`, `httpx`, `redis`, `psycopg[binary]`, `PyYAML`, `ujson`);
- `kubectl`, `gh`, `jq`, `ripgrep`, `rsync`;
- `docker` CLI for image builds/pushes (the daemon runs on the node via a mounted socket);
- `codexctl` built and installed into `/usr/local/bin`.

Why this matters: the Codex agent runs in a Kubernetes pod without access to
host tools. Missing binaries (kubectl/gh/docker/rsync/protoc, etc.) will break
preflight checks and block apply/build/test workflows.

You can reference this image in `images.codex` and use it in `services.codex` inside your project's `services.yaml`
(in the examples — `codex-project`):

- a `codex` Pod in each dev-AI slot will run this image;
- inside the Pod you have `codex`, `codexctl`, `kubectl`, `gh` and other tools available.

---

## 🛡️ 9. Security and stability

- **Early development stage.** `codexctl` is at an early stage of development, test coverage is limited, and unstable
  behavior and breaking changes are possible. Use the tool carefully and plan time for debugging.
- **Isolated clusters only.** The assumption is that `codexctl` and Codex agents run in a Kubernetes cluster
  **separate from production**, dedicated to development and AI experiments (dev/staging/ai). **Do not use** it directly
  on a live production cluster.
- **Limit external access.** Dev/staging/dev-AI environments must be protected:
  - HTTP interfaces are hidden behind an OAuth2 proxy or another authentication mechanism;
  - ingresses and services must not be directly accessible from the internet without authorization;
  - access to the kube API is restricted by users/roles.
- **Codex agent permissions.** The `codex` Pod gets elevated permissions in the slot namespace (creating/updating
  deployments, reading logs, `exec` into Pods, etc.). Make sure to:
  - review the RBAC manifests (Role/RoleBinding) in `deploy/codex` for your project;
  - not grant the agent permissions to manage critical namespaces;
  - keep kubeconfig and secrets only in protected storages (GitHub secrets, Kubernetes secrets, Vault).
- **Use with care.** Automatic changes to the cluster and repository performed by the Codex agent via `codexctl`
  must go through human review. Design processes so that any changes made by the agent go through PRs
  and manual approval.

If you integrate `codexctl` into a new project (`codex-project` or another), start with a small, isolated stack,
gradually extend scenarios and add checks (manual review, smoke tests, dedicated namespaces/clusters for experiments).
