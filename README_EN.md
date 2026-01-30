<!--
This file is an English translation of README.md (Russian).
If you update README.md, please update this file as well.
-->

<div align="center">
  <img src="docs/media/logo.png" alt="PAI logo" width="120" height="120" />
  <h1>codexctl</h1>
  <p>🧠 A tool for managing cloud-based planning and development workflows in a Kubernetes cluster via AI agents, based on <a href="https://github.com/openai/codex">OpenAI’s codex-cli</a> and GitHub workflows.</p>
  <p>🚧 Alpha version. Breaking changes are possible.</p>
</div>

![Go Version](https://img.shields.io/github/go-mod/go-version/codex-k8s/codexctl)
[![Go Reference](https://pkg.go.dev/badge/github.com/codex-k8s/codexctl.svg)](https://pkg.go.dev/github.com/codex-k8s/codexctl)

🇷🇺 Русская версия: [README.md](README.md)

`codexctl` is a CLI tool for declarative management of Kubernetes environments and AI-dev slots from a single configuration
file, `services.yaml`. It simplifies:

- deploying infrastructure (databases, caches, ingress, observability) and applications in Kubernetes projects;
- preparing temporary AI-dev environments for tasks/PRs where a Codex agent works;
- rendering manifests and configs (including Codex config) using templates.

In essence, it is an “orchestrator on top of `kubectl` and templates” that understands:

- environments (`dev`, `staging`, `ai`, `ai-repair`);
- slots (AI environments for tasks/PRs);
- the project layout (infrastructure, services, Codex agent Pod).

> Important: the utility is at an early stage; see “Security and stability” at the end.

## 🎯 Goal and ideal DX for an AI agent

`codexctl` is designed as a “button” for cloud AI development and planning in Kubernetes: for each Issue/PR, an isolated
environment (namespace/slot) is created with the same stack as the project (services, DBs, caches, queues, ingress,
observability), and the agent works *inside* the cluster next to that stack.

This provides a practical “like a real developer” experience, but without having to install the entire environment locally:

- the agent makes HTTP requests to services in the cluster and verifies behavior and contracts;
- inspects logs/events, metrics, rollout statuses;
- connects to PostgreSQL/Redis/queues and checks migrations and data;
- applies infrastructure/services declaratively via `services.yaml` and `codexctl apply/ci apply`.

Working example (ready-made `services.yaml` and GitHub Actions workflows): https://github.com/codex-k8s/project-example

---

## 📦 Installation

Local Go toolchain requirements:

- Go **>= 1.25.1** (see `go.mod`).

For instructions on preparing a VPS/self-hosted runner (microk8s, Docker, kubectl, gh, rsync, etc.), see:
https://github.com/codex-k8s/project-example/blob/main/README_RU.md

`codexctl` is distributed as a Go CLI. With Go installed, you can install it with:

```bash
go install github.com/codex-k8s/codexctl/cmd/codexctl@latest
```

Or install a specific version (replace `v999.999.999` with an actual SemVer tag):

```bash
go install github.com/codex-k8s/codexctl/cmd/codexctl@v999.999.999
```

Go package documentation is available on pkg.go.dev: https://pkg.go.dev/github.com/codex-k8s/codexctl.

---

## 🚨 Important: external binary dependencies

Right now, `codexctl` **depends on external CLI tools** and runs them as subprocesses. This intentionally simplifies
bootstrapping and integration with existing practices (kubectl/gh/git/docker), but it requires those binaries to be
installed and available in `PATH` (both on the self-hosted runner and inside the Codex container).

Minimum required tools:

- `kubectl` — apply/delete manifests, `wait`, diagnostics (see `internal/kube/*`, `hooks: kubectl.wait`);
- `bash` — executing hook steps `run:` (see `internal/hooks/*`);
- `docker` — `images mirror/build` (pull/tag/push/build) (see `internal/cli/images.go`);
- `git` — commit/push in PR flow (see `internal/cli/pr.go`);
- `gh` — reading/commenting Issues/PRs and GraphQL/REST calls (see `internal/githubapi/*`, `internal/cli/*`).

Optional:

- `rsync` — speeds up source sync (if missing, a slower fallback copier is used) (see `internal/cli/manage_env.go`).

Environment check: use `codexctl doctor` (it checks for `kubectl`, `bash`, `git`, `gh`, and `docker` when an `images`
block is present in `services.yaml`, and warns if `rsync` is missing).

Future plan: gradually replace some external dependencies with built-in implementations (Kubernetes/GitHub/OCI clients,
sync logic, etc.) via SDKs/libraries, to reduce the set of required binaries and make runs more predictable.

For a practical guide to installing the required tools on a VPS for a runner, see:
https://github.com/codex-k8s/project-example/blob/main/README_RU.md

---

## 💡 1. Key ideas

### 📦 1.1. One `services.yaml` for the whole project

Instead of disparate Helm charts and bash scripts, a single `services.yaml` file is used, which describes:

- which images to use and how to build them (`images`);
- which infrastructure manifests to apply (`infrastructure`);
- which services to deploy (`services`);
- what environments look like (`environments`), namespaces and slots (`namespace`, `state`);
- how the Codex agent Pod is configured (`codex`).

This file is the single source of truth for `codexctl`, GitHub Actions, and AI-dev environments.

### 🧩 1.2. Templating and context

`services.yaml` and all included manifests are rendered via Go templates. In templates you can use:

- `{{ .Env }}` — the current environment (`dev`, `staging`, `ai`, `ai-repair`);
- `{{ .Namespace }}` — the Kubernetes namespace;
- `{{ .Project }}` — the project name (`codex-project`);
- `{{ .Slot }}` — the slot number for an AI-dev environment;
- `{{ .BaseDomain }}` — a map of base domains (`dev`, `staging`, `ai`, `ai-repair`);
- `{{ .Versions }}` — a map of service/image versions;
- functions `envOr`, `default`, `ternary`, `join`, etc.

The same context is also used by:

- manifest rendering (`codexctl apply` / `codexctl ci apply`);
- built-in prompt templates (`internal/prompt/templates/*.tmpl`);
- the Codex config template (`internal/prompt/templates/config_default.toml`, or an overridden one).

### 🌐 1.3. Environments and slots

`codexctl` works with environment types:

- `dev` — a developer’s local environment (one namespace);
- `staging` — a staging cluster (CI/CD, close to production);
- `ai` — AI-dev slots: isolated namespaces like `<project>-dev-<slot>` (for example, `codex-project-dev-<slot>`),
  with domains `dev-<slot>.staging.<domain>`, where Codex agents work on tasks/PRs.
- `ai-repair` — a separate namespace with a Codex Pod and RBAC access to the staging namespace (for recovery/repair).

Slots (`slot`) are numeric identifiers for AI-dev environments managed by `codexctl ci ensure-slot/ensure-ready`. For each
slot, the following is created and maintained:

- a separate namespace;
- a separate set of PVCs/data (`.data/slots/<slot>` on the host);
- a separate `codex` Pod with the agent image and your project sources mounted (in examples: `codex-project`).

---

### 🧪 1.4. Issue flow and the agent’s role

The basic idea is:

- you create an Issue in the repository and apply a specific label, e.g. `[ai-plan]` for planning or `[ai-dev]` for development;
- a GitHub Actions workflow reacts to that label, calls `codexctl ci ensure-slot/ensure-ready --env ai ...`, and deploys the
  full stack of the project’s infrastructure and services into a separate namespace;
- in that namespace, a `codex` Pod is started with a Codex agent, and `codexctl prompt run` feeds it a prompt of the required
  type (`kind=plan_issue` or `kind=dev_issue`, languages: `ru`/`en`).

The key feature of the approach is that the agent works **in a live environment** and “debugs” its changes the same way a
developer would:

- reads service logs via `kubectl logs`;
- connects to DBs and caches (via `psql`, `redis-cli`, or custom CLI/HTTP/gRPC clients);
- performs real requests to the project’s HTTP/gRPC endpoints;
- can run tests, migrations, load fixtures, restart deployments.

Each AI-dev environment is isolated (its own namespace and data), so the agent does not interfere with other developers and
does not touch services of other developers/agents.

### 🏷️ 1.5. Issue labels and how they affect the agent instructions

This project uses two classes of labels:

1) **Trigger labels (workflow labels)** — control which type of agent/session will be started:
- `[ai-plan]` — planning mode (the agent prepares a plan/Issue structure, without PRs and commits);
- `[ai-dev]` — development mode (the agent changes code, makes commits, and opens a PR);
- `[ai-repair]` — environment recovery/repair mode (staging/infrastructure) and a PR if needed.

> Important: the agent **must not** add trigger labels `[ai-dev]`, `[ai-plan]`, `[ai-repair]` by itself unless the user explicitly asked for it.

2) **Semantic task labels** — describe the type of work and affect how the agent formulates its plan/actions.
These labels can be applied together (multiple at once):
- `feature` — planning/implementing new functionality (including refactors, new services, etc.);
- `bug` — finding the cause and/or fixing a bug/incorrect logic;
- `doc` — writing/updating documentation;
- `debt` — addressing technical debt (refactoring, dependency updates, quality improvements);
- `idea` — brainstorming/elaborating an idea (multiple variants, questions, discussion in comments);
- `epic` — a large epic task, split into subtasks.

3) **Model/reasoning configuration labels** — allow selecting the agent model and reasoning effort
   (supported on both Issues and PRs; priority: CLI flags → Issue → PR):
- model: `[ai-model-gpt-5.2-codex]`, `[ai-model-gpt-5.2]`, `[ai-model-gpt-5.1-codex-max]`, `[ai-model-gpt-5.1-codex-mini]`;
- reasoning: `[ai-reasoning-low]`, `[ai-reasoning-medium]`, `[ai-reasoning-high]`, `[ai-reasoning-extra-high]`.

How this is used in agent instructions:
- In **planning** modes (`[ai-plan]`), the agent uses these labels to structure the plan (feature/bug/doc/debt/idea) and may
  create new Issues/epics/subtasks *only if the user asks for that format*. To link child tasks, the marker
  `AI-PLAN-PARENT: #<root>` is used in the Issue body.
- In **development** modes (`[ai-dev]`), the agent follows the semantics of labels (feature/bug/doc/debt) during
  implementation and verification; if needed, it may create additional Issues for discovered side tasks (e.g. `bug`/`doc`/`debt`)
  without derailing the main task.
- In **plan review** modes, the agent responds to the user’s comments and, if the user asks to refine the result,
  **edits the existing result** (comment/Issue body) rather than creating a new one (unless additional variants were requested).

## 🚀 2. Quick start

### ✅ 2.1. Requirements

- A Kubernetes cluster (separate from production).
- `kubectl` and kubeconfig available for the selected environment.
- A Docker daemon and (recommended) a local image registry.
- The `codexctl` binary in `PATH`.

### 📝 2.2. Minimal `services.yaml` for a project

The simplest example (in the current format; see also `services.yaml` in https://github.com/codex-k8s/project-example):

```yaml
# {{- $codeRootBase := envOr "CODE_ROOT_BASE" "" -}}
# {{- $slotCodeRoot := default $codeRootBase (printf "%s/slots" .ProjectRoot) -}}
# {{- $stagingCodeRoot := default (ternary (ne $codeRootBase "") (printf "%s/staging/src" $codeRootBase) "") .ProjectRoot -}}
# {{- $dataRoot := default (envOr "DATA_ROOT" "") (printf "%s/.data" .ProjectRoot) -}}

project: project-example

codex:
  promptLang: "ru"
  extraTools: [psql, redis-cli]
  links:
    - title: Chat frontend
      path: /
    - title: Django admin
      path: /admin/
  projectContext: |
    - Before starting, read ./AGENTS.md and relevant docs in docs/*.md.
    - When working with manifests, use `codexctl render` and `codexctl apply` only with filters `--only-services/--only-infra` (or `--skip-*`).
  servicesOverview: |
    - Django backend: admin UI and PostgreSQL DB migrations.
    - Go chat backend: chat HTTP API, auth, working with PostgreSQL and Redis.
    - Web frontend: SPA chat UI.
  timeouts:
    exec: "60m"
    rollout: "30m"

baseDomain:
  dev: '{{ envOr "BASE_DOMAIN_DEV" "dev.example-domain.ru" }}'
  staging: '{{ envOr "BASE_DOMAIN_STAGING" "staging.example-domain.ru" }}'
  ai: '{{ envOr "BASE_DOMAIN_AI" (envOr "BASE_DOMAIN_STAGING" "staging.example-domain.ru") }}'
  ai-repair: '{{ envOr "BASE_DOMAIN_STAGING" "staging.example-domain.ru" }}'

namespace:
  patterns:
    dev: "{{ .Project }}-dev"
    staging: "{{ .Project }}-staging"
    ai: "{{ .Project }}-dev-{{ .Slot }}"
    ai-repair: "{{ .Project }}-ai-repair-{{ .Slot }}"

registry: '{{ envOr "REGISTRY_HOST" "localhost:32000" }}'

dataPaths:
  root: '{{ $dataRoot }}'
  envDir: '{{ ternary (eq .Env "ai") (printf "%s/slots/%d" $dataRoot .Slot) (printf "%s/%s" $dataRoot .Env) }}'
  dirs: [postgres, redis]

state:
  backend: configmap
  configmapNamespace: codex-system
  configmapPrefix: codex-env-

environments:
  dev:
    kubeconfig: "/home/user/.kube/project-example-dev"
    imagePullPolicy: IfNotPresent
  staging:
    kubeconfig: "/home/runner/.kube/microk8s.config"
    imagePullPolicy: Always
  ai:
    from: "staging"
    imagePullPolicy: IfNotPresent
  ai-repair:
    from: "staging"
    imagePullPolicy: IfNotPresent

images:
  postgres:
    type: external
    from: "docker.io/library/postgres:16-bookworm"
    local: '{{ envOr "REGISTRY_HOST" "localhost:32000" }}/library/postgres:16-bookworm'
  # Service build images are described similarly (dockerfile/context/buildArgs/tagTemplate)

infrastructure:
  - name: namespace-and-config
    when: '{{ or (eq .Env "dev") (eq .Env "staging") (eq .Env "ai") (eq .Env "ai-repair") }}'
    manifests:
      - path: deploy/namespace.yaml
      - path: deploy/configmap.yaml
      - path: deploy/secret.yaml

services:
  - name: chat-backend
    manifests:
      - path: services/chat_backend/deploy.yaml
    overlays:
      ai:
        hostMounts:
          - name: go-src
            hostPath: '{{ printf "%s/%d/src/services/chat_backend" $slotCodeRoot .Slot }}'
            mountPath: "/app"
        dropKinds: ["Ingress"]
```

In a real project, blocks will be richer (versions, hooks, overlays), but the basic principle is the same.

### 🔁 2.3. Base deployment cycle

For any environment (`dev`, `staging`, `ai`, `ai-repair`), the cycle is the same:

```bash
ENV=staging   # or dev/ai

codexctl images mirror --env "$ENV"    # if needed
codexctl images build  --env "$ENV"    # build and push images from images.type=build

# It is recommended to apply only via filters (and separately for infra/services).
codexctl apply --env "$ENV" --only-infra namespace-and-config,data-services,observability,cluster-dns,tls-issuer,echo-probe --wait --preflight
codexctl apply --env "$ENV" --only-services django-backend,chat-backend,web-frontend --wait
```

Infrastructure group and service names come from your project’s `services.yaml`; the examples use values from `project-example`.

When using GitHub Actions, this cycle is embedded into the workflow — see the integration section.

---

## 📑 3. `services.yaml` format

`services.yaml` is a “manifest of manifests” for your project. Below is an overview of the key blocks.

### 🌱 3.1. Root fields

- `project` — project code, used in namespaces and other templates.
- `envFiles` — a list of `.env` files with environment variables that are loaded during rendering.
- `registry` — the base registry address (e.g. `localhost:32000`).
- `versions` — a version dictionary (arbitrary keys, used in templates).

### 🤖 3.2. The `codex` block

Configuration for integration with the Codex agent:

- `codex.configTemplate` — path to a Codex config template (e.g. `deploy/codex/config.toml`). If not specified, the built-in
  `internal/prompt/templates/config_default.toml` is used.
- `codex.links` — a list of links (title + path) that will be rendered in environment comments (e.g. Swagger, Admin).
- `codex.extraTools` — a list of additional CLI tools available in the agent image and useful for prompts
  (e.g. `psql`, `redis-cli`, `k6`).
- `codex.projectContext` — free-form text about project specifics (where to read docs, how to run tests, etc.);
  it is inserted into prompts (see built-in templates).
- `codex.servicesOverview` — an overview of infrastructure/application services and their URLs/ports; also included in prompts.
- `codex.timeouts.exec`/`codex.timeouts.rollout` — timeouts for `prompt run` and for waiting on rollouts.

These fields are used when rendering built-in prompts (`dev_issue_*`, `plan_issue_*`, `plan_review_*`, `dev_review_*`,
`ai-repair_*`) and the Codex config:

- `internal/prompt/templates/*.tmpl` — prompt templates;
- `internal/prompt/templates/config_default.toml` — default Codex config.

You can override:

- the Codex config via `codex.configTemplate`;
- the prompts themselves — by providing your own `--template` for `codexctl prompt ...` or by replacing the built-in `.tmpl`
  files in the image.

### 🌐 3.3. `baseDomain` and `namespace`

```yaml
baseDomain:
  dev: "dev.codex-project.local"
  staging: "staging.codex-project.local"
  ai: "staging.codex-project.local"
  ai-repair: "staging.codex-project.local"

namespace:
  patterns:
    dev: "{{ .Project }}-dev"
    staging: "{{ .Project }}-staging"
    ai: "{{ .Project }}-dev-{{ .Slot }}"
    ai-repair: "{{ .Project }}-ai-repair-{{ .Slot }}"
```

- `baseDomain` — domains for ingresses by environment.
- `namespace.patterns` — namespace templates; for `ai` the default is `project-dev-<slot>`.
### 🗺️ 3.4. `environments`

Cluster connection configuration:

```yaml
environments:
  dev:
    kubeconfig: "/home/user/.kube/project-example-dev"
    imagePullPolicy: IfNotPresent
    localRegistry:
      enabled: true
      name: "project-example-registry"
      port: 32000
  staging:
    kubeconfig: "/home/runner/.kube/microk8s.config"
    imagePullPolicy: Always
    localRegistry:
      enabled: true
      name: "project-example-registry"
      port: 32000
  ai:
    from: "staging"
    imagePullPolicy: IfNotPresent
  ai-repair:
    from: "staging"
    imagePullPolicy: IfNotPresent
```

- `from` allows inheriting settings (e.g. `ai` from `staging`).
- `localRegistry` describes a local registry to which images will be pushed by `images build`.

### 🖼️ 3.5. `images`

Describes external and buildable images:

```yaml
images:
  busybox:
    type: external
    from: 'docker.io/library/busybox:{{ index .Versions "busybox" }}'
    local: '{{ envOr "REGISTRY_HOST" "localhost:32000" }}/library/busybox:{{ index .Versions "busybox" }}'

  chat-backend:
    type: build
    repository: '{{ envOr "REGISTRY_HOST" "localhost:32000" }}/project-example/chat-backend'
    tagTemplate: '{{ printf "%s-%s" (ternary (eq .Env "ai") "staging" .Env) (index .Versions "chat-backend") }}'
    dockerfile: 'services/chat_backend/Dockerfile'
    context: 'services/chat_backend'
    buildArgs:
      GOLANG_IMAGE_VERSION: '{{ index .Versions "golang" }}'
      SERVICE_VERSION: '{{ index .Versions "chat-backend" }}'
```

- `type: external` — images mirrored via `images mirror`;
- `type: build` — images built and pushed via `images build`.

### 🏗️ 3.6. `infrastructure`

A list of infrastructure services:

```yaml
infrastructure:
  - name: namespace-and-config
    when: '{{ or (eq .Env "dev") (eq .Env "staging") (eq .Env "ai") (eq .Env "ai-repair") }}'
    manifests:
      - path: deploy/namespace.yaml
      - path: deploy/configmap.yaml
      - path: deploy/secret.yaml

  - name: data-services
    when: '{{ or (eq .Env "dev") (eq .Env "staging") (eq .Env "ai") }}'
    manifests:
      - path: deploy/postgres.service.yaml
      - path: deploy/redis.service.yaml
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
- may contain `hooks.beforeApply/afterApply/afterDestroy` that call `kubectl` or shell scripts.

### 🧱 3.7. `services`

A list of applications:

```yaml
# {{- $codeRootBase := envOr "CODE_ROOT_BASE" "" -}}
# {{- $slotCodeRoot := default $codeRootBase (printf "%s/slots" .ProjectRoot) -}}
# {{- $stagingCodeRoot := default (ternary (ne $codeRootBase "") (printf "%s/staging/src" $codeRootBase) "") .ProjectRoot -}}

services:
  - name: chat-backend
    manifests:
      - path: services/chat_backend/deploy.yaml
    image:
      repository: '{{ envOr "REGISTRY_HOST" "localhost:32000" }}/project-example/chat-backend'
      tagTemplate: '{{ printf "%s-%s" (ternary (eq .Env "ai") "staging" .Env) (index .Versions "chat-backend") }}'
    overlays:
      dev:
        hostMounts:
          - name: go-src
            hostPath: "{{ .ProjectRoot }}/services/chat_backend"
            mountPath: "/app"
      staging:
        hostMounts:
          - name: go-src
            hostPath: '{{ printf "%s/services/chat_backend" $stagingCodeRoot }}'
            mountPath: "/app"
      ai:
        hostMounts:
          - name: go-src
            hostPath: '{{ printf "%s/%d/src/services/chat_backend" $slotCodeRoot .Slot }}'
            mountPath: "/app"
        dropKinds: ["Ingress"]
```

- `manifests` — a list of YAML files for the service;
- `image` — overrides `image:` in manifests (repository/tag);
- `overlays` — per-environment settings (hostPath source mounts, disabling ingress in AI-dev, etc.).
- `hostMounts` — a list of directories mounted from the host (local sources for dev/AI-dev).
  Optional: `hostPathType` (default `Directory`). For `/var/run/docker.sock`, use `Socket`.
- `dropKinds` — a list of Kubernetes resources (by kind) to drop from rendering (e.g. Ingress in AI-dev).

---

## 🛠️ 4. Applying manifests

### ☸️ 4.1. `codexctl apply`
```bash
# staging (example for project-example)
codexctl apply --env staging \
  --only-infra namespace-and-config,data-services,observability,cluster-dns,tls-issuer,echo-probe \
  --wait --preflight

codexctl apply --env staging \
  --only-services django-backend,chat-backend,web-frontend \
  --wait

# AI-dev slot
codexctl apply --env ai --slot 123 \
  --only-services chat-backend \
  --wait --preflight
```

The command:

- renders the stack;
- performs preflight checks (if enabled with `--preflight`);
- applies manifests via `kubectl apply`;
- runs `afterApply` hooks (e.g. waiting for rollouts);
- if `--wait` is set, waits for deployments to become ready.

Filters for safer application:

- `--only-services name1,name2` — apply only selected services;
- `--skip-services name1,name2` — skip selected services;
- `--only-infra name1,name2` — apply only selected infrastructure groups;
- `--skip-infra name1,name2` — skip selected infrastructure groups.

When running inside the Codex Pod, always use filters and do not apply the `codex` service.
Additionally (often important specifically inside the Codex Pod): use `--skip-infra tls-issuer,echo-probe` to avoid
cluster-scope resources and local port checks (see built-in prompts `*_issue_*.tmpl`).

### 🧩 4.2. `codexctl render`

Renders manifests without applying them:

```bash
codexctl render \
  --env staging \
  --only-services web-frontend
```

---

## ⌨️ 5. `codexctl` commands: overview

### ⚙️ 5.1. Global flags

- `--config, -c` — path to `services.yaml` (default: `services.yaml` in the current directory).
- `--env` — environment name (`dev`, `staging`, `ai`, `ai-repair`).
- `--namespace` — explicit namespace override (usually not needed).
- `--log-level` — log level (`debug`, `info`, `warn`, `error`).

### ☸️ 5.2. `apply`

- Purpose: render and apply the stack to Kubernetes.
- Typical example: see section 4.1.

### 🧩 5.3. `render`

- Purpose: render manifests to stdout without applying.
- Convenient in CI or inside the Codex Pod to inspect results.

### 🧪 5.4. `ci`

A set of commands for CI scenarios and slot preparation.

Subcommands:

- `ci images` — mirrors external images and/or builds local ones for CI.
  Flags: `--mirror/--build` (both `true` by default), `--slot`, `--vars`, `--var-file`.
- `ci apply` — applies manifests with retries and optional waiting.
  Flags: `--preflight`, `--wait`, `--apply-retries`, `--wait-retries`, `--apply-backoff`, `--wait-backoff`,
  `--wait-timeout`, `--request-timeout`, plus render filters (`--only-services/--skip-services/--only-infra/--skip-infra`).
- `ci ensure-slot` — allocates/reuses a slot by selector `--issue/--pr/--slot` (one is required).
  Output: `plain|json|kv`.
- `ci ensure-ready` — ensures a slot and, if needed, syncs sources, prepares images, and applies manifests.
  Flags: `--code-root-base`, `--source`, `--prepare-images`, `--apply`, `--force-apply`, `--wait-timeout`,
  `--wait-soft-fail`, output `plain|json|kv`.
  With `--code-root-base` and `--source`, sources are synced to `<code-root-base>/<slot>/src`.

### 🖼️ 5.5. `images`

Subcommands:

- `images mirror` — mirrors `images.type=external` to a local registry:

  ```bash
  codexctl images mirror --env staging
  ```

- `images build` — builds and pushes `images.type=build`:

  ```bash
  codexctl images build --env staging
  ```

### 🎛️ 5.6. `manage-env`

A group of commands for metadata and cleanup of AI-dev slots (`env=ai`):

- `manage-env cleanup` — deletes a slot environment and state records.
- `manage-env set` — sets slot ↔ issue/PR links.
- `manage-env comment` — renders environment links for comments.

Notes:

- `manage-env cleanup` supports `--all` (clean up all matching slots) and `--with-configmap` (delete the state ConfigMap for
  selected environments).
- `manage-env comment` accepts `--lang en|ru` for the comment language.

### 🧠 5.7. `prompt`

Commands for working with Codex agent prompts:

- `prompt run` — runs the Codex agent in the `codex` Pod:

  ```bash
  codexctl prompt run \
    --env ai \
    --slot 1 \
    --kind dev_issue \
    --lang ru
  ```

  Uses built-in prompt templates (`internal/prompt/templates/dev_issue_*.tmpl`) and `services.yaml` context
  (`codex.extraTools`, `codex.projectContext`, `codex.servicesOverview`, `codex.links`).

Notes:

- `prompt run` supports `--issue`/`--pr` context, `--resume` mode, `--infra-unhealthy`, plus `--vars`, `--var-file`.
- You can also set model and reasoning effort via `--model` and `--reasoning-effort`.
- Allowed models: `gpt-5.2-codex`, `gpt-5.2`, `gpt-5.1-codex-max`, `gpt-5.1-codex-mini`.
- Allowed reasoning effort values: `low`, `medium`, `high`, `extra-high`.
- `--template` overrides `--kind`; if `--kind` is not set, `dev_issue` is used by default.

### 🧭 5.8. `plan`

Commands for working with plans and linked-task structure:

- `plan resolve-root` — find the “parent” planning Issue for a specific task:

  ```bash
  codexctl plan resolve-root \
    --issue 123 \
    --repo owner/codex-project \
    --output json
  ```

  The command uses:
  - the `[ai-plan]` label on the root planning Issue;
  - the `AI-PLAN-PARENT: #<root>` marker in the body of child Issues.

This makes it possible to build a tree of tasks: one planning Issue with `[ai-plan]` describes architecture and stages, and
child Issues with `AI-PLAN-PARENT: #<root>` are implemented by separate AI-dev slots (`[ai-dev]`) via `ci ensure-ready` and
`prompt run`.

### 🔄 5.9. `pr review-apply`

- Automatically applies changes made by the Codex agent in an AI-dev environment to a PR:

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
  - posts a comment on the PR with links to the environment.

---

## 🌍 6. Environment variables

`codexctl` uses a merged map of variables:

- process variables (`os.Environ()`);
- variables from `envFiles` in `services.yaml`;
- variables from `--var-file` and `--vars`.

Via `envOr`, these variables are available in templates:

```yaml
registry: '{{ envOr "REGISTRY_HOST" "localhost:32000" }}'
```

Common variables:

- `KUBECONFIG` — path to kubeconfig if not set in `environments.*.kubeconfig`;
- `REGISTRY_HOST` — image registry host;
- `CODE_ROOT_BASE` — base path to source directories (on the node/in CI), used to compute:
  - `slotCodeRoot` (e.g. `.../slots/<slot>/src/...`) and
  - `stagingCodeRoot` (e.g. `.../staging/src/...`),
  which are then used in `services.*.overlays.*.hostMounts` (see header comments in `services.yaml`).
- `DATA_ROOT` — base path to `.data` with Postgres/Redis/cache/etc. data (used in `dataPaths.root` and `dataPaths.envDir`).
  It is cleaned up by `manage-env cleanup --with-configmap` (in AI-dev).
In GitHub Actions, you typically set:

- `GITHUB_RUN_ID`, `GITHUB_REPOSITORY`, `DEV_SLOTS_MAX` — to link slots and CI runs;
- secrets for connecting to DB/Redis/caches and other external services;
- `CODEX_GH_PAT`, `CODEX_GH_USERNAME` — token and username for the GitHub bot;
- `CONTEXT7_API_KEY` — Context7 API key (if used);
- `OPENAI_API_KEY` — OpenAI API key.

---

## 🔐 7. GitHub Actions integration and secrets

Below are workflow examples used in the example project (see also `project-example` repo: `.github/workflows/*.yml`).
It assumes a self-hosted runner where the following are already installed: `codexctl`, `kubectl`, `gh`, `rsync`, `docker`.

### 🚀 7.1. Deploy staging (push to `main`)

```yaml
name: "Staging deploy 🚀"

on:
  push:
    branches: [main]

concurrency:
  group: staging-deploy
  cancel-in-progress: false

jobs:
  deploy:
    name: "Deploy staging via codexctl 🚀"
    if: >
      !contains(github.event.head_commit.message, '[skip ci]') &&
      !contains(github.event.head_commit.message, '[skip-ci]') &&
      !contains(github.event.head_commit.message, '[no ci]') &&
      !contains(github.event.head_commit.message, '[no-ci]')
    runs-on: self-hosted
    environment: staging
    steps:
      - name: "Checkout project-example 📥"
        uses: actions/checkout@v4
        with:
          ref: ${{ github.sha }}
          token: ${{ secrets.CODEX_GH_PAT }}

      - name: "Sync staging sources 📂"
        env:
          CODE_ROOT_BASE: ${{ vars.CODE_ROOT_BASE }}
        run: |
          set -euo pipefail
          if [ -z "${CODE_ROOT_BASE:-}" ]; then
            echo "error: CODE_ROOT_BASE is not set" >&2
            exit 1
          fi
          STAGING_SRC="${CODE_ROOT_BASE}/staging/src"
          mkdir -p "${STAGING_SRC}"
          rsync -a --delete \
            --exclude '.cache' \
            ./ "${STAGING_SRC}/"

      - name: "Prepare images via codexctl 🪞🏗️"
        env:
          REGISTRY_HOST: localhost:32000
        run: |
          set -euo pipefail
          codexctl ci images --env staging --mirror --build

      - name: "Apply staging via codexctl 🚀"
        env:
          KUBECONFIG:           /home/runner/.kube/microk8s.config
          NO_PROXY:             127.0.0.1,localhost,::1
          GITHUB_RUN_ID:        ${{ github.run_id }}
          CODEX_GH_PAT:         ${{ secrets.CODEX_GH_PAT }}
          CODEX_GH_USERNAME:    ${{ vars.CODEX_GH_USERNAME }}
          OPENAI_API_KEY:       ${{ secrets.OPENAI_API_KEY }}
          CONTEXT7_API_KEY:     ${{ secrets.CONTEXT7_API_KEY }}
          CODE_ROOT_BASE:       ${{ vars.CODE_ROOT_BASE }}
          DATA_ROOT:            ${{ vars.DATA_ROOT }}
          POSTGRES_USER:        ${{ secrets.POSTGRES_USER }}
          POSTGRES_PASSWORD:    ${{ secrets.POSTGRES_PASSWORD }}
          REDIS_PASSWORD:       ${{ secrets.REDIS_PASSWORD }}
          SECRET_KEY:           ${{ secrets.SECRET_KEY }}
        run: |
          set -euo pipefail
          codexctl ci apply --env staging --preflight --wait

  gc-registry:
    needs: deploy
    runs-on: self-hosted
    environment: staging
    steps:
      - name: "Checkout 📥"
        uses: actions/checkout@v4
        with:
          token: ${{ secrets.CODEX_GH_PAT }}

      - name: "GC docker registry container 🗑️"
        run: |
          set -euo pipefail
          NAME="${DOCKER_REGISTRY_CONTAINER:-project-example-registry}"
          if ! docker ps --format '{{.Names}}' | grep -q "^${NAME}$"; then
            echo "info: registry container ${NAME} not running" >&2
            exit 0
          fi
          echo "info: running registry GC inside ${NAME} (--delete-untagged=true)" >&2
          set +e
          docker exec "${NAME}" registry garbage-collect /etc/docker/registry/config.yml --delete-untagged=true
          RC=$?
          set -e
          if [[ $RC -ne 0 ]]; then
            echo "warn: GC with --delete-untagged failed; retrying without flag" >&2
            docker exec "${NAME}" registry garbage-collect /etc/docker/registry/config.yml || true
          fi
          echo "info: GC finished" >&2
        shell: bash
```

### 🧭 7.2. AI Plan (planning by Issue: label `[ai-plan]`)

Key ideas:

- the workflow triggers only for `[ai-plan]` and only for actors listed in `AI_ALLOWED_USERS`;
- it creates/finds a slot for the Issue and brings up an AI-dev environment via `ci ensure-ready`;
- it runs the planning agent via `prompt run --kind plan_issue`;
- on failure, it cleans up the slot via `manage-env cleanup`.
```yaml
name: "AI Plan 🧭"

on:
  issues:
    types: [labeled]

env:
  AI_ALLOWED_USERS: ${{ vars.AI_ALLOWED_USERS }}
  CODEX_GH_USERNAME: ${{ vars.CODEX_GH_USERNAME }}

concurrency:
  group: ai-plan-${{ github.event.issue.number }}
  cancel-in-progress: false

jobs:
  create-ai-plan:
    if: >-
      github.event.label.name == '[ai-plan]' &&
      contains(format(',{0},', vars.AI_ALLOWED_USERS), format(',{0},', github.actor))
    runs-on: self-hosted
    environment: staging
    outputs:
      slot: ${{ steps.alloc.outputs.slot }}
      namespace: ${{ steps.alloc.outputs.namespace }}
    steps:
      - uses: actions/checkout@v4
        with:
          token: ${{ secrets.CODEX_GH_PAT }}
      - id: alloc
        env:
          GITHUB_RUN_ID:     ${{ github.run_id }}
          CODEX_GH_PAT:      ${{ secrets.CODEX_GH_PAT }}
          CODEX_GH_USERNAME: ${{ vars.CODEX_GH_USERNAME }}
        run: |
          set -euo pipefail
          ENV_NAME="ai"
          ISSUE="${{ github.event.issue.number }}"
          MAX="${{ vars.DEV_SLOTS_MAX }}"
          ARGS=(ci ensure-slot --env "${ENV_NAME}" --issue "${ISSUE}" --output kv)
          if [ -n "$MAX" ]; then ARGS+=(--max "$MAX"); fi
          OUT="$(codexctl "${ARGS[@]}")"
          echo "$OUT"
          echo "slot=$(echo \"$OUT\" | sed -n 's/^slot=//p')" >> "$GITHUB_OUTPUT"
          echo "namespace=$(echo \"$OUT\" | sed -n 's/^namespace=//p')" >> "$GITHUB_OUTPUT"

  deploy-ai-plan:
    needs: [create-ai-plan]
    runs-on: self-hosted
    environment: staging
    outputs:
      infra_ready: ${{ steps.ensure.outputs.infra_ready }}
    steps:
      - uses: actions/checkout@v4
        with:
          ref: ${{ github.sha }}
          token: ${{ secrets.CODEX_GH_PAT }}
      - id: ensure
        env:
          GITHUB_RUN_ID:        ${{ github.run_id }}
          CODEX_GH_PAT:         ${{ secrets.CODEX_GH_PAT }}
          CODEX_GH_USERNAME:    ${{ vars.CODEX_GH_USERNAME }}
          OPENAI_API_KEY:       ${{ secrets.OPENAI_API_KEY }}
          CONTEXT7_API_KEY:     ${{ secrets.CONTEXT7_API_KEY }}
          CODE_ROOT_BASE:       ${{ vars.CODE_ROOT_BASE }}
          DATA_ROOT:            ${{ vars.DATA_ROOT }}
          POSTGRES_USER:        ${{ secrets.POSTGRES_USER }}
          POSTGRES_PASSWORD:    ${{ secrets.POSTGRES_PASSWORD }}
          REDIS_PASSWORD:       ${{ secrets.REDIS_PASSWORD }}
          SECRET_KEY:           ${{ secrets.SECRET_KEY }}
        run: |
          set -euo pipefail
          SLOT="${{ needs.create-ai-plan.outputs.slot }}"
          ISSUE="${{ github.event.issue.number }}"
          export CODEX_WORKSPACE_UID="$(id -u)"
          export CODEX_WORKSPACE_GID="$(id -g)"
          OUT="$(codexctl ci ensure-ready --env ai --slot "${SLOT}" --issue "${ISSUE}" --code-root-base "${CODE_ROOT_BASE}" --source "." --prepare-images --apply --force-apply --wait-soft-fail --output kv --vars "CODE_ROOT_BASE=${CODE_ROOT_BASE},DATA_ROOT=${DATA_ROOT}")"
          echo "$OUT"
          echo "infra_ready=$(echo \"$OUT\" | sed -n 's/^infraReady=//p')" >> "$GITHUB_OUTPUT"

  run-codex-plan:
    needs: [create-ai-plan, deploy-ai-plan]
    runs-on: self-hosted
    environment: staging
    steps:
      - uses: actions/checkout@v4
        with:
          token: ${{ secrets.CODEX_GH_PAT }}
      - env:
          GITHUB_RUN_ID:     ${{ github.run_id }}
          CODEX_GH_PAT:      ${{ secrets.CODEX_GH_PAT }}
          CODEX_GH_USERNAME: ${{ vars.CODEX_GH_USERNAME }}
          OPENAI_API_KEY:    ${{ secrets.OPENAI_API_KEY }}
          CONTEXT7_API_KEY:  ${{ secrets.CONTEXT7_API_KEY }}
        run: |
          set -euo pipefail
          SLOT="${{ needs.create-ai-plan.outputs.slot }}"
          NS="${{ needs.create-ai-plan.outputs.namespace }}"
          ISSUE="${{ github.event.issue.number }}"
          INFRA_READY="${{ needs.deploy-ai-plan.outputs.infra_ready }}"
          ARGS=(prompt run --env ai --slot "${SLOT}" --kind plan_issue --lang ru)
          if [ -n "$NS" ]; then ARGS+=(--namespace "$NS"); fi
          if [ -n "$ISSUE" ]; then ARGS+=(--issue "$ISSUE"); fi
          if [ "$INFRA_READY" = "false" ] || [ "$INFRA_READY" = "0" ]; then ARGS+=(--infra-unhealthy); fi
          codexctl "${ARGS[@]}"
```

### 👁 7.3. AI Plan Review (review planning results via comments)

Trigger: a new comment in an Issue (not a PR) that contains `[ai-plan]`. The workflow does:

1) `codexctl plan resolve-root` — find the root planning Issue for the current one (subtask/epic).
2) `ci ensure-ready --issue <ROOT>` — bring up the environment (if not already up).
3) `prompt run --kind plan_review` with `FOCUS_ISSUE_NUMBER=<...>` — focus the agent on a specific task/comment.

```yaml
name: "AI Plan Review 👁"

on:
  issue_comment:
    types: [created]

jobs:
  run:
    if: >
      github.event.issue.pull_request == null &&
      contains(github.event.comment.body, '[ai-plan]')
    runs-on: self-hosted
    environment: staging
    env:
      CODE_ROOT_BASE: ${{ vars.CODE_ROOT_BASE }}
      DATA_ROOT:      ${{ vars.DATA_ROOT }}
    steps:
      - uses: actions/checkout@v4
        with:
          token: ${{ secrets.CODEX_GH_PAT }}

      - name: "Resolve root planning issue 🔗"
        id: root_issue
        env:
          FOCUS_ISSUE_NUMBER: ${{ github.event.issue.number }}
          GITHUB_REPOSITORY:  ${{ github.repository }}
          CODEX_GH_PAT:       ${{ secrets.CODEX_GH_PAT }}
        run: |
          set -euo pipefail
          OUT="$(codexctl plan resolve-root --issue "${FOCUS_ISSUE_NUMBER}" --repo "${GITHUB_REPOSITORY}" --output kv)"
          echo "$OUT"
          echo "ROOT_ISSUE_NUMBER=$(echo \"$OUT\" | sed -n 's/^root=//p')" >> "$GITHUB_OUTPUT"
          echo "FOCUS_ISSUE_NUMBER=$(echo \"$OUT\" | sed -n 's/^focus=//p')" >> "$GITHUB_OUTPUT"

      - name: "Ensure env ready (root issue) 📇"
        id: card
        env:
          ROOT_ISSUE_NUMBER: ${{ steps.root_issue.outputs.ROOT_ISSUE_NUMBER }}
        run: |
          set -euo pipefail
          export CODEX_WORKSPACE_UID="$(id -u)"
          export CODEX_WORKSPACE_GID="$(id -g)"
          OUT="$(codexctl ci ensure-ready --env ai --issue "${ROOT_ISSUE_NUMBER}" --code-root-base "${CODE_ROOT_BASE}" --source "." --prepare-images --apply --output kv --vars "CODE_ROOT_BASE=${CODE_ROOT_BASE},DATA_ROOT=${DATA_ROOT}")"
          echo "$OUT"
          echo "SLOT=$(echo \"$OUT\" | sed -n 's/^slot=//p' | head -n1)" >> "$GITHUB_OUTPUT"
          echo "NS=$(echo \"$OUT\" | sed -n 's/^namespace=//p' | head -n1)" >> "$GITHUB_OUTPUT"
          CREATED="$(echo \"$OUT\" | sed -n 's/^created=//p' | head -n1)"
          RECREATED="$(echo \"$OUT\" | sed -n 's/^recreated=//p' | head -n1)"
          if [ "$CREATED" = "true" ] || [ "$RECREATED" = "true" ]; then
            echo "NEW_ENV=true" >> "$GITHUB_OUTPUT"
          else
            echo "NEW_ENV=false" >> "$GITHUB_OUTPUT"
          fi

      - name: "Run plan review agent 🤖"
        env:
          ROOT_ISSUE_NUMBER:  ${{ steps.root_issue.outputs.ROOT_ISSUE_NUMBER }}
          FOCUS_ISSUE_NUMBER: ${{ steps.root_issue.outputs.FOCUS_ISSUE_NUMBER }}
        run: |
          set -euo pipefail
          SLOT_VAL="${{ steps.card.outputs.SLOT }}"
          NS_VAL="${{ steps.card.outputs.NS }}"
          NEW_ENV="${{ steps.card.outputs.NEW_ENV }}"
          ROOT="${ROOT_ISSUE_NUMBER}"
          FOCUS="${FOCUS_ISSUE_NUMBER}"
          RESUME_FLAG="--resume"
          VARS="FOCUS_ISSUE_NUMBER=${FOCUS}"
          if [ "${NEW_ENV}" = "true" ]; then
            RESUME_FLAG=""
            VARS="FOCUS_ISSUE_NUMBER=${FOCUS},PROMPT_CONTINUATION=1,PROMPT_MODE=full"
          fi
          ARGS=(prompt run --env ai --slot "${SLOT_VAL}" --kind plan_review --lang ru)
          if [ -n "${NS_VAL}" ]; then ARGS+=(--namespace "${NS_VAL}"); fi
          if [ -n "${ROOT}" ]; then ARGS+=(--issue "${ROOT}"); fi
          ARGS+=(--vars "${VARS}")
          if [ -n "${RESUME_FLAG}" ]; then ARGS+=("${RESUME_FLAG}"); fi
          codexctl "${ARGS[@]}"
```
### 🛠 7.4. AI Dev by Issue (label `[ai-dev]`)

Workflow:

1) Check that the label is `[ai-dev]` and the actor is in `AI_ALLOWED_USERS`.
2) `ci ensure-slot --env ai --issue <N>` — select/create a slot (respecting `DEV_SLOTS_MAX`).
3) `ci ensure-ready --env ai --slot <SLOT> --issue <N> --prepare-images --apply` — bring up the AI-dev environment.
4) Prepare a working branch in the slot workspace (`codex/issue-<N>`).
5) `prompt run --kind dev_issue` — run the dev agent (if infra is unhealthy, add `--infra-unhealthy`).
6) auto-commit → push, find the PR by branch, attach the PR to the slot (`manage-env set`) and post a comment with links
   (`manage-env comment` + `gh pr comment`).
7) On failure — cleanup (`manage-env cleanup --with-configmap`).

```yaml
name: "AI Dev Issue 🛠"

on:
  issues:
    types: [labeled]

env:
  AI_ALLOWED_USERS: ${{ vars.AI_ALLOWED_USERS }}
  CODEX_GH_USERNAME: ${{ vars.CODEX_GH_USERNAME }}

concurrency:
  group: ai-issue-${{ github.event.issue.number }}
  cancel-in-progress: false

jobs:
  create-ai:
    if: >-
      github.event.label.name == '[ai-dev]' &&
      contains(format(',{0},', vars.AI_ALLOWED_USERS), format(',{0},', github.actor))
    runs-on: self-hosted
    environment: staging
    outputs:
      slot: ${{ steps.alloc.outputs.slot }}
      namespace: ${{ steps.alloc.outputs.namespace }}
    steps:
      - uses: actions/checkout@v4
        with:
          token: ${{ secrets.CODEX_GH_PAT }}

      - id: alloc
        env:
          GITHUB_RUN_ID:     ${{ github.run_id }}
          CODEX_GH_PAT:      ${{ secrets.CODEX_GH_PAT }}
          CODEX_GH_USERNAME: ${{ vars.CODEX_GH_USERNAME }}
        run: |
          set -euo pipefail
          ISSUE="${{ github.event.issue.number }}"
          MAX="${{ vars.DEV_SLOTS_MAX }}"
          ARGS=(ci ensure-slot --env ai --issue "${ISSUE}" --output kv)
          if [ -n "$MAX" ]; then ARGS+=(--max "$MAX"); fi
          OUT="$(codexctl "${ARGS[@]}")"
          echo "$OUT"
          echo "slot=$(echo "$OUT" | sed -n 's/^slot=//p' | head -n1)" >> "$GITHUB_OUTPUT"
          echo "namespace=$(echo "$OUT" | sed -n 's/^namespace=//p' | head -n1)" >> "$GITHUB_OUTPUT"

  deploy-ai:
    needs: [create-ai]
    runs-on: self-hosted
    environment: staging
    outputs:
      infra_ready: ${{ steps.ensure.outputs.infra_ready }}
    steps:
      - uses: actions/checkout@v4
        with:
          ref: ${{ github.sha }}
          token: ${{ secrets.CODEX_GH_PAT }}

      - id: ensure
        env:
          CODE_ROOT_BASE:       ${{ vars.CODE_ROOT_BASE }}
          DATA_ROOT:            ${{ vars.DATA_ROOT }}
          # ... plus CODEX_GH_PAT / OPENAI_API_KEY / app secrets (see project-example)
        run: |
          set -euo pipefail
          SLOT="${{ needs.create-ai.outputs.slot }}"
          ISSUE="${{ github.event.issue.number }}"
          MAX="${{ vars.DEV_SLOTS_MAX }}"
          export CODEX_WORKSPACE_UID="$(id -u)"
          export CODEX_WORKSPACE_GID="$(id -g)"
          ARGS=(ci ensure-ready --env ai --slot "${SLOT}" --issue "${ISSUE}" --code-root-base "${CODE_ROOT_BASE}" --source "." --prepare-images --apply --force-apply --wait-soft-fail --output kv --vars "CODE_ROOT_BASE=${CODE_ROOT_BASE},DATA_ROOT=${DATA_ROOT}")
          if [ -n "$MAX" ]; then ARGS+=(--max "$MAX"); fi
          OUT="$(codexctl "${ARGS[@]}")"
          echo "$OUT"
          echo "infra_ready=$(echo "$OUT" | sed -n 's/^infraReady=//p' | head -n1)" >> "$GITHUB_OUTPUT"

  run-codex:
    needs: [create-ai, deploy-ai]
    runs-on: self-hosted
    environment: staging
    env:
      CODE_ROOT_BASE: ${{ vars.CODE_ROOT_BASE }}
      CODEX_GH_PAT:   ${{ secrets.CODEX_GH_PAT }}
    steps:
      - uses: actions/checkout@v4
        with:
          token: ${{ secrets.CODEX_GH_PAT }}

      - name: "Ensure working branch 🌿"
        env:
          SLOT: ${{ needs.create-ai.outputs.slot }}
        run: |
          set -euo pipefail
          ISSUE_NUMBER="${{ github.event.issue.number }}"
          cd "${CODE_ROOT_BASE}/${SLOT}/src"
          git config user.name "codex-bot"
          git config user.email "codex-bot@example.com"
          git checkout -b "codex/issue-${ISSUE_NUMBER}" || git checkout "codex/issue-${ISSUE_NUMBER}"

      - name: "Run Codex dev agent 🤖"
        env:
          SLOT:  ${{ needs.create-ai.outputs.slot }}
          NS:    ${{ needs.create-ai.outputs.namespace }}
          ISSUE: ${{ github.event.issue.number }}
          INFRA_READY: ${{ needs.deploy-ai.outputs.infra_ready }}
        run: |
          set -euo pipefail
          ARGS=(prompt run --env ai --slot "${SLOT}" --kind dev_issue --lang ru)
          if [ -n "$NS" ]; then ARGS+=(--namespace "$NS"); fi
          if [ -n "$ISSUE" ]; then ARGS+=(--issue "$ISSUE"); fi
          if [ "$INFRA_READY" = "false" ] || [ "$INFRA_READY" = "0" ]; then ARGS+=(--infra-unhealthy); fi
          codexctl "${ARGS[@]}"

      - name: "Auto-commit/push + PR link/comment"
        run: |
          set -euo pipefail
          # ... (commit/push; detect PR; manage-env set; manage-env comment; gh pr comment)
          true

  cleanup-ai:
    needs: [create-ai, deploy-ai, run-codex]
    if: always()
    runs-on: self-hosted
    environment: staging
    steps:
      - uses: actions/checkout@v4
        with:
          token: ${{ secrets.CODEX_GH_PAT }}
      - run: |
          set -euo pipefail
          # ... cleanup on failure only
          true
```

Full example: `project-example` repo, `.github/workflows/ai_dev_issue.yml`.

### 👁 7.5. AI PR Review (auto-fix on Changes Requested)

Trigger: a submitted review with state `changes_requested`. The workflow brings up an environment for the PR, runs the
`dev_review` agent, then applies changes and comments on the PR via `codexctl pr review-apply`.

```yaml
name: "AI PR Review 👁"

on:
  pull_request_review:
    types: [submitted]

env:
  AI_ALLOWED_USERS: ${{ vars.AI_ALLOWED_USERS }}
  CODEX_GH_USERNAME: ${{ vars.CODEX_GH_USERNAME }}

concurrency:
  group: ai-pr-${{ github.event.pull_request.number }}
  cancel-in-progress: false

jobs:
  run:
    if: >-
      github.event.review.state == 'changes_requested' &&
      contains(format(',{0},', vars.AI_ALLOWED_USERS), format(',{0},', github.actor))
    runs-on: self-hosted
    environment: staging
    env:
      CODE_ROOT_BASE:       ${{ vars.CODE_ROOT_BASE }}
      CODEX_GH_PAT:         ${{ secrets.CODEX_GH_PAT }}
      DATA_ROOT:            ${{ vars.DATA_ROOT }}
      CODEX_GH_USERNAME:    ${{ vars.CODEX_GH_USERNAME }}
      OPENAI_API_KEY:       ${{ secrets.OPENAI_API_KEY }}
      CONTEXT7_API_KEY:     ${{ secrets.CONTEXT7_API_KEY }}
      # ... plus app secrets (POSTGRES_*, REDIS_PASSWORD, SECRET_KEY, etc.)
    steps:
      - uses: actions/checkout@v4
        with:
          ref: ${{ github.event.pull_request.head.ref }}
          token: ${{ secrets.CODEX_GH_PAT }}
          fetch-depth: 0

      - id: env
        run: |
          set -euo pipefail
          PR_NUMBER="${{ github.event.pull_request.number }}"

          echo "info: ensuring AI PR review environment ready via codexctl (ensure-ready)" >&2
          export CODEX_WORKSPACE_UID="$(id -u)"
          export CODEX_WORKSPACE_GID="$(id -g)"
          OUT="$(codexctl ci ensure-ready --env ai --pr "${PR_NUMBER}" --code-root-base "${CODE_ROOT_BASE}" --source "." --prepare-images --apply --output kv --vars "CODE_ROOT_BASE=${CODE_ROOT_BASE},DATA_ROOT=${DATA_ROOT}")"
          echo "$OUT"
          echo "SLOT=$(echo "$OUT" | sed -n 's/^slot=//p' | head -n1)" >> "$GITHUB_OUTPUT"
          echo "NS=$(echo "$OUT" | sed -n 's/^namespace=//p' | head -n1)" >> "$GITHUB_OUTPUT"
          CREATED="$(echo "$OUT" | sed -n 's/^created=//p' | head -n1)"
          RECREATED="$(echo "$OUT" | sed -n 's/^recreated=//p' | head -n1)"
          if [ "$CREATED" = "true" ] || [ "$RECREATED" = "true" ]; then
            echo "NEW_ENV=true" >> "$GITHUB_OUTPUT"
          else
            echo "NEW_ENV=false" >> "$GITHUB_OUTPUT"
          fi

      - name: "Run Codex review-fix agent 🤖"
        run: |
          set -euo pipefail
          SLOT="${{ steps.env.outputs.SLOT }}"
          NS="${{ steps.env.outputs.NS }}"
          NEW_ENV="${{ steps.env.outputs.NEW_ENV }}"
          PR="${{ github.event.pull_request.number }}"
          KIND="dev_review"
          RESUME_FLAG="--resume"
          VARS=""
          if [ "${NEW_ENV}" = "true" ]; then
            RESUME_FLAG=""
            VARS="PROMPT_CONTINUATION=1,PROMPT_MODE=full"
          fi
          ARGS=(prompt run --env ai --slot "${SLOT}" --kind "${KIND}" --lang ru --pr "${PR}")
          if [ -n "$NS" ]; then ARGS+=(--namespace "$NS"); fi
          if [ -n "$VARS" ]; then ARGS+=(--vars "$VARS"); fi
          if [ -n "$RESUME_FLAG" ]; then ARGS+=("$RESUME_FLAG"); fi
          codexctl "${ARGS[@]}"

      - name: "Apply review changes and comment 💾"
        run: |
          set -euo pipefail
          SLOT="${{ steps.env.outputs.SLOT }}"
          PR_NUMBER="${{ github.event.pull_request.number }}"
          codexctl pr review-apply --env ai --slot "${SLOT}" --pr "${PR_NUMBER}" --code-root-base "${CODE_ROOT_BASE}" --lang ru
```

Full example: `project-example` repo, `.github/workflows/ai_pr_review.yml`.
### 🧯 7.6. Staging Repair by Issue (label `[ai-repair]`)

This mode brings up an `ai-repair` environment (Codex pod + RBAC to the staging namespace), syncs staging sources, runs the
`ai-repair_issue` agent, and if needed pushes changes to the `codex/ai-repair-<N>` branch.

```yaml
name: "Staging Repair 🧯"

on:
  issues:
    types: [labeled]

jobs:
  run:
    if: github.event.label.name == '[ai-repair]'
    runs-on: self-hosted
    steps:
      - uses: actions/checkout@v4
        with:
          token: ${{ secrets.CODEX_GH_PAT }}

      - id: alloc
        run: |
          set -euo pipefail
          OUT="$(codexctl ci ensure-slot --env ai-repair --issue "${{ github.event.issue.number }}" --output kv)"
          SLOT="$(echo "$OUT" | sed -n 's/^slot=//p' | head -n1)"
          echo "slot=$SLOT" >> "$GITHUB_OUTPUT"

      - run: |
          set -euo pipefail
          codexctl ci apply --env ai-repair --slot "${{ steps.alloc.outputs.slot }}" --preflight --wait --only-infra namespace-and-config,codex-ai-repair-rbac --only-services codex

      - run: codexctl prompt run --env ai-repair --slot "${{ steps.alloc.outputs.slot }}" --kind ai-repair_issue --lang ru --issue "${{ github.event.issue.number }}"
```


Full example: `project-example` repo, `.github/workflows/ai_repair_issue.yml`.

### 👁 7.7. Staging Repair PR Review (Changes Requested for `codex/ai-repair-*`)

Trigger: `changes_requested` in a review and the PR branch starts with `codex/ai-repair-`. The workflow ensures an
`ai-repair` environment and runs `ai-repair_review`, then applies fixes via `codexctl pr review-apply`.

```yaml
name: "Staging Repair PR Review 👁"

on:
  pull_request_review:
    types: [submitted]

jobs:
  run:
    if: >
      github.event.review.state == 'changes_requested' &&
      startsWith(github.event.pull_request.head.ref, 'codex/ai-repair-')
    runs-on: self-hosted
    steps:
      - uses: actions/checkout@v4
        with:
          ref: ${{ github.event.pull_request.head.ref }}
          token: ${{ secrets.CODEX_GH_PAT }}
          fetch-depth: 0

      - id: alloc
        run: |
          set -euo pipefail
          OUT="$(codexctl ci ensure-slot --env ai-repair --pr "${{ github.event.pull_request.number }}" --output kv)"
          SLOT="$(echo "$OUT" | sed -n 's/^slot=//p' | head -n1)"
          echo "slot=$SLOT" >> "$GITHUB_OUTPUT"

      - run: codexctl ci apply --env ai-repair --slot "${{ steps.alloc.outputs.slot }}" --preflight --wait --only-infra namespace-and-config,codex-ai-repair-rbac --only-services codex
      - run: codexctl prompt run --env ai-repair --slot "${{ steps.alloc.outputs.slot }}" --kind ai-repair_review --lang ru --pr "${{ github.event.pull_request.number }}" --resume
      - run: codexctl pr review-apply --env ai-repair --slot "${{ steps.alloc.outputs.slot }}" --pr "${{ github.event.pull_request.number }}" --code-root-base "${{ vars.CODE_ROOT_BASE }}" --lang ru
```

Full example: `project-example` repo, `.github/workflows/ai_repair_pr_review.yml`.

### 🧹 7.8. Cleanup (closing Issue/PR)

When an Issue/PR is closed, the workflow cleans up environments (`manage-env cleanup`) and deletes branches `codex/issue-*` /
`codex/ai-repair-*`. If the PR was merged, the workflow additionally closes the linked Issue (by number parsed from the
branch name).

```yaml
name: "AI Cleanup 🧹"

on:
  pull_request:
    types: [closed]
  issues:
    types: [closed]

jobs:
  cleanup:
    runs-on: self-hosted
    steps:
      - uses: actions/checkout@v4
        with:
          token: ${{ secrets.CODEX_GH_PAT }}
      - if: github.event_name == 'pull_request'
        run: codexctl manage-env cleanup --env ai --pr "${{ github.event.pull_request.number }}" --with-configmap || true
      - if: github.event_name == 'issues'
        run: codexctl manage-env cleanup --env ai --issue "${{ github.event.issue.number }}" --with-configmap || true
```

Full example: `project-example` repo, `.github/workflows/ai_cleanup.yml`.

### 🔑 7.9. Secrets and PAT for the GitHub bot

Recommended set of secrets/vars in your project repository (e.g. `codex-project`):

- `CODEX_GH_PAT` — PAT for a GitHub bot user;
- `CODEX_GH_USERNAME` — bot username. Do not use a developer’s personal account; create a dedicated technical account.
- `KUBECONFIG` / paths to kubeconfig for staging;
- secrets for DB/Redis/cache/queue (username/password, DSN, etc.);
- `REGISTRY_HOST` and (optionally) registry credentials.
- `OPENAI_API_KEY` — OpenAI API key.
- `CONTEXT7_API_KEY` — Context7 API key (if used).
- `AI_ALLOWED_USERS` (vars) — list of allowed GitHub users, in the format `user1,user2,user3`.
- `DEV_SLOTS_MAX` (vars) — maximum number of slots that `ci ensure-slot/ensure-ready` can allocate.

How to create a user and PAT:

1. Create a separate technical GitHub account for the bot (e.g. `codex-bot-42`).
2. In account settings, open **Developer settings → Personal access tokens → Fine-grained**.
3. Create a token with permissions:
   - access to the project repository (e.g. `codex-project`, read/write for `code`, `pull requests`, `issues`);
   - access to Actions (if you need to manage workflows).
4. Save the token and add it to the repository secrets as `CODEX_GH_PAT`.

---

## 🐳 8. Codex agent image (example project)

An example Dockerfile for the agent image is available in the example project repository:
`github.com/codex-k8s/project-example/deploy/codex/Dockerfile`.

It contains everything the agent needs inside the pod:

- Node + Codex CLI (`@openai/codex`);
- Go toolchain + plugins (`protoc-gen-go`, `protoc-gen-go-grpc`, `wire`);
- `protoc` and standard includes;
- Python + a virtual environment with basic libraries (`requests`, `httpx`, `redis`, `psycopg[binary]`, `PyYAML`, `ujson`);
- `kubectl`, `gh`, `jq`, `ripgrep`, `rsync`;
- `docker` CLI for building/pushing images (the daemon runs on the node via a mounted socket);
- build of `codexctl` and installation to `/usr/local/bin`.

Why it matters: the Codex agent works inside a Kubernetes pod and has no access to host tools. Missing binaries
(kubectl/gh/docker/rsync/protoc, etc.) break preflight checks and block apply/build/test scenarios.

You can reference such an image in `images.codex` and use it in `services.codex` inside your project’s `services.yaml`
(in examples: `codex-project`):

- the `codex` Pod in each AI-dev slot will run with that image;
- inside the Pod, `codex`, `codexctl`, `kubectl`, `gh`, and other tools are available.

---

## 🛡️ 9. Security and stability

- **Early development stage.** `codexctl` is in its early stages; there is no test coverage yet; unstable behavior and
  breaking changes are possible. Use it cautiously and budget time for debugging.
- **Isolated clusters only.** It is assumed that `codexctl` and Codex agents work in a Kubernetes cluster **separate from
  production**, intended for development and AI experiments (dev/staging/ai). **Do not use** it directly on top of a live
  production cluster.
- **Restrict external access.** Dev/staging/AI-dev environments must be protected:
  - HTTP interfaces are hidden behind OAuth2-proxy/IAP or another authentication mechanism;
  - ingresses and services must not be directly accessible from the internet without authorization;
  - access to the kube API is restricted by users/roles.
- **Codex agent permissions.** The `codex` Pod gets elevated permissions in the slot namespace (create/update deployments,
  read logs, `exec` into Pods, etc.). Make sure to:
  - review RBAC manifests (Role/RoleBinding) under `deploy/codex` in your project;
  - not grant the agent permissions to manage critical namespaces;
  - store kubeconfig and secrets only in protected storages (GitHub secrets, Kubernetes secrets, Vault).
- **Use with care.** Automatic cluster and repository changes performed by a Codex agent via `codexctl` should be reviewed
  by humans. Design processes so that any changes made by an agent go through a PR and manual approval.

If you integrate `codexctl` into a new project (`codex-project` or another), start with a small isolated stack and gradually
expand scenarios and add safeguards (manual review, smoke tests, separate namespaces/clusters for experiments).
