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
- a GitHub Actions workflow reacts to that label, calls `codexctl ci ensure-slot/ensure-ready` (parameters come from
  `CODEXCTL_*`), and deploys the full stack of the project’s infrastructure and services into a separate namespace;
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
   (supported on both Issues and PRs; priority: CLI flags → Issue → PR → environment variables → services.yaml → config.toml defaults):
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
# {{- $codeRootBase := envOr "CODEXCTL_CODE_ROOT_BASE" "" -}}
# {{- $slotCodeRoot := default $codeRootBase (printf "%s/slots" .ProjectRoot) -}}
# {{- $stagingCodeRoot := default (ternary (ne $codeRootBase "") (printf "%s/staging/src" $codeRootBase) "") .ProjectRoot -}}
# {{- $dataRoot := default (envOr "CODEXCTL_DATA_ROOT" "") (printf "%s/.data" .ProjectRoot) -}}

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
export CODEXCTL_ENV=staging   # or dev/ai
# for ai also set: CODEXCTL_SLOT=<slot>

codexctl images mirror    # if needed
codexctl images build     # build and push images from images.type=build

# It is recommended to apply only via filters (and separately for infra/services).
codexctl apply --only-infra namespace-and-config,data-services,observability,cluster-dns,tls-issuer,echo-probe --wait --preflight
codexctl apply --only-services django-backend,chat-backend,web-frontend --wait
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
# {{- $codeRootBase := envOr "CODEXCTL_CODE_ROOT_BASE" "" -}}
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
export CODEXCTL_ENV=staging
codexctl apply \
  --only-infra namespace-and-config,data-services,observability,cluster-dns,tls-issuer,echo-probe \
  --wait --preflight

codexctl apply \
  --only-services django-backend,chat-backend,web-frontend \
  --wait

# AI-dev slot
export CODEXCTL_ENV=ai
export CODEXCTL_SLOT=123
codexctl apply \
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
export CODEXCTL_ENV=staging
codexctl render \
  --only-services web-frontend
```

---

## ⌨️ 5. `codexctl` commands: overview

### ⚙️ 5.1. Global flags

You can provide values via `CODEXCTL_*` env vars; flags take precedence.

- `CODEXCTL_CONFIG` / `--config, -c` — path to `services.yaml` (default: `services.yaml` in the current directory).
- `CODEXCTL_ENV` / `--env` — environment name (`dev`, `staging`, `ai`, `ai-repair`).
- `CODEXCTL_NAMESPACE` / `--namespace` — explicit namespace override (usually not needed).
- `CODEXCTL_LOG_LEVEL` / `--log-level` — log level (`debug`, `info`, `warn`, `error`).

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
  Parameters come from `CODEXCTL_*` (e.g. `CODEXCTL_MIRROR_IMAGES`, `CODEXCTL_BUILD_IMAGES`, `CODEXCTL_SLOT`,
  `CODEXCTL_VARS`, `CODEXCTL_VAR_FILE`).
- `ci apply` — applies manifests with retries and optional waiting.
  Parameters come from `CODEXCTL_*` (e.g. `CODEXCTL_PREFLIGHT`, `CODEXCTL_WAIT`, `CODEXCTL_APPLY_RETRIES`,
  `CODEXCTL_WAIT_RETRIES`, `CODEXCTL_APPLY_BACKOFF`, `CODEXCTL_WAIT_BACKOFF`, `CODEXCTL_WAIT_TIMEOUT`,
  `CODEXCTL_REQUEST_TIMEOUT`, plus render filters `CODEXCTL_ONLY_SERVICES/CODEXCTL_SKIP_SERVICES/CODEXCTL_ONLY_INFRA/CODEXCTL_SKIP_INFRA`).
- `ci sync-sources` — syncs sources into the workspace.
  Parameters come from `CODEXCTL_*` (e.g. `CODEXCTL_CODE_ROOT_BASE`, `CODEXCTL_SOURCE`, `CODEXCTL_ENV`, `CODEXCTL_SLOT`).
- `ci ensure-slot` — allocates/reuses a slot by selector `CODEXCTL_ISSUE_NUMBER`/`CODEXCTL_PR_NUMBER`/`CODEXCTL_SLOT` (one is required).
  When `GITHUB_OUTPUT` is set, it writes `slot`, `namespace`, `env` outputs for GitHub Actions.
- `ci ensure-ready` — ensures a slot and, if needed, syncs sources, prepares images, and applies manifests.
  Parameters come from `CODEXCTL_*` (e.g. `CODEXCTL_CODE_ROOT_BASE`, `CODEXCTL_SOURCE`, `CODEXCTL_PREPARE_IMAGES`,
  `CODEXCTL_APPLY`, `CODEXCTL_FORCE_APPLY`, `CODEXCTL_WAIT_TIMEOUT`, `CODEXCTL_WAIT_SOFT_FAIL`). When `GITHUB_OUTPUT`
  is set, it writes `slot`, `namespace`, `env`, `created`, `recreated`, `infra_ready`, `codexctl_env_ready`, `infra_unhealthy`, `codexctl_new_env`, `codexctl_run_args` (boolean fields are `true/false`).
  With `CODEXCTL_CODE_ROOT_BASE` and `CODEXCTL_SOURCE`, sources are synced to `<CODEXCTL_CODE_ROOT_BASE>/<slot>/src`.

### 🖼️ 5.5. `images`

Subcommands:

- `images mirror` — mirrors `images.type=external` to a local registry:

  ```bash
  export CODEXCTL_ENV=staging
  codexctl images mirror
  ```

- `images build` — builds and pushes `images.type=build`:

  ```bash
  export CODEXCTL_ENV=staging
  codexctl images build
  ```

### 🎛️ 5.6. `manage-env`

A group of commands for metadata and cleanup of AI-dev slots (`env=ai`):

- `manage-env cleanup` — deletes a slot environment and state records.
- `manage-env cleanup-pr` — cleans environments by PR and (optionally) deletes the branch/closes a linked issue.
- `manage-env cleanup-issue` — cleans environments by Issue and (optionally) deletes `codex/*` branches.
- `manage-env close-linked-issue` — closes an Issue inferred from a `codex/issue-*` or `codex/ai-repair-*` branch name.
- `manage-env set` — sets slot ↔ issue/PR links.
- `manage-env comment` — renders environment links for comments.
- `manage-env comment-pr` — renders and posts a comment with links to a PR.

Notes:

- `manage-env cleanup` supports `CODEXCTL_ALL` / `--all` (clean up all matching slots) and
  `CODEXCTL_WITH_CONFIGMAP` / `--with-configmap` (delete the state ConfigMap for selected environments).
- `manage-env comment` and `manage-env comment-pr` accept `CODEXCTL_LANG` / `--lang en|ru` for the comment language.

### 🧠 5.7. `prompt`

Commands for working with Codex agent prompts:

- `prompt run` — runs the Codex agent in the `codex` Pod:

  ```bash
  export CODEXCTL_ENV=ai
  export CODEXCTL_SLOT=1
  export CODEXCTL_LANG=ru
  codexctl prompt run --kind dev_issue
  ```

  Uses built-in prompt templates (`internal/prompt/templates/dev_issue_*.tmpl`) and `services.yaml` context
  (`codex.extraTools`, `codex.projectContext`, `codex.servicesOverview`, `codex.links`).

Notes:

- `prompt run` takes context from `CODEXCTL_ISSUE_NUMBER` / `CODEXCTL_PR_NUMBER`, resume mode from `CODEXCTL_RESUME`,
  infra degradation from `CODEXCTL_INFRA_UNHEALTHY`, extra vars from `CODEXCTL_VARS` / `CODEXCTL_VAR_FILE`
  (flags are still supported, but CI should prefer `CODEXCTL_*`).
- `CODEXCTL_LANG` sets the language for prompts and tool messages.
- You can also set model and reasoning effort via `--model` and `--reasoning-effort`.
- Environment variables: `CODEXCTL_MODEL`, `CODEXCTL_MODEL_REASONING_EFFORT` (lower priority than flags and labels).
- Allowed models: `gpt-5.2-codex`, `gpt-5.2`, `gpt-5.1-codex-max`, `gpt-5.1-codex-mini`.
- Allowed reasoning effort values: `low`, `medium`, `high`, `extra-high`.
- `--template` overrides `--kind`; if `--kind` is not set, `dev_issue` is used by default.

### 🧭 5.8. `plan`

Commands for working with plans and linked-task structure:

- `plan resolve-root` — find the “parent” planning Issue for a specific task:

  ```bash
  CODEXCTL_ISSUE_NUMBER=123 \
  CODEXCTL_REPO=owner/codex-project \
  codexctl plan resolve-root \

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
  CODEXCTL_ENV=ai \
  CODEXCTL_SLOT=1 \
  CODEXCTL_PR_NUMBER=42 \
  CODEXCTL_CODE_ROOT_BASE="/srv/codex/envs" \
  CODEXCTL_LANG=ru \
  codexctl pr review-apply
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
- `CODEXCTL_CODE_ROOT_BASE` — base path to source directories (on the node/in CI), used to compute:
  - `slotCodeRoot` (e.g. `.../slots/<slot>/src/...`) and
  - `stagingCodeRoot` (e.g. `.../staging/src/...`),
  which are then used in `services.*.overlays.*.hostMounts` (see header comments in `services.yaml`).
- `CODEXCTL_DATA_ROOT` — base path to `.data` with Postgres/Redis/cache/etc. data (used in `dataPaths.root` and `dataPaths.envDir`).
  It is cleaned up by `manage-env cleanup` with `CODEXCTL_WITH_CONFIGMAP=true` (in AI-dev).
In GitHub Actions, you typically set:

- `GITHUB_RUN_ID`, `CODEXCTL_REPO`, `CODEXCTL_DEV_SLOTS_MAX` — to link slots and CI runs;
- secrets for connecting to DB/Redis/caches and other external services;
- `CODEXCTL_GH_PAT`, `CODEXCTL_GH_USERNAME` — token and username for the GitHub bot;
- `CODEXCTL_GH_EMAIL` — bot email for git commits (for example, `codex-bot@example.com`).
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
          token: ${{ secrets.CODEXCTL_GH_PAT }}

      - name: "Sync staging sources 📂"
        env:
          CODEXCTL_CODE_ROOT_BASE: ${{ vars.CODEXCTL_CODE_ROOT_BASE }}
        run: |
          set -euo pipefail
          codexctl ci sync-sources

      - name: "Prepare images via codexctl 🪞🏗️"
        env:
          CODEXCTL_ENV:          staging
          CODEXCTL_MIRROR_IMAGES: true
          CODEXCTL_BUILD_IMAGES:  true
          REGISTRY_HOST: localhost:32000
        run: |
          set -euo pipefail
          codexctl ci images

      - name: "Apply staging via codexctl 🚀"
        env:
          KUBECONFIG:           /home/runner/.kube/microk8s.config
          NO_PROXY:             127.0.0.1,localhost,::1
          GITHUB_RUN_ID:        ${{ github.run_id }}
          CODEXCTL_GH_PAT:         ${{ secrets.CODEXCTL_GH_PAT }}
          CODEXCTL_GH_USERNAME: ${{ vars.CODEXCTL_GH_USERNAME }}
          CODEXCTL_GH_EMAIL:    ${{ vars.CODEXCTL_GH_EMAIL }}
          CODEXCTL_ENV:            staging
          CODEXCTL_PREFLIGHT:      true
          CODEXCTL_WAIT:           true
          OPENAI_API_KEY:       ${{ secrets.OPENAI_API_KEY }}
          CONTEXT7_API_KEY:     ${{ secrets.CONTEXT7_API_KEY }}
          CODEXCTL_CODE_ROOT_BASE:       ${{ vars.CODEXCTL_CODE_ROOT_BASE }}
          CODEXCTL_DATA_ROOT:            ${{ vars.CODEXCTL_DATA_ROOT }}
          POSTGRES_USER:        ${{ secrets.POSTGRES_USER }}
          POSTGRES_PASSWORD:    ${{ secrets.POSTGRES_PASSWORD }}
          REDIS_PASSWORD:       ${{ secrets.REDIS_PASSWORD }}
          SECRET_KEY:           ${{ secrets.SECRET_KEY }}
        run: |
          set -euo pipefail
          codexctl ci apply

  gc-registry:
    needs: deploy
    runs-on: self-hosted
    environment: staging
    steps:
      - name: "Checkout 📥"
        uses: actions/checkout@v4
        with:
          token: ${{ secrets.CODEXCTL_GH_PAT }}

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

- the workflow triggers only for `[ai-plan]` and only for actors listed in `CODEXCTL_ALLOWED_USERS`;
- it creates/finds a slot for the Issue and brings up an AI-dev environment via `ci ensure-ready`;
- it runs the planning agent via `prompt run --kind plan_issue`;
- on failure, it cleans up the slot via `manage-env cleanup`.
```yaml
name: "AI Plan 🧭"

on:
  issues:
    types: [labeled]

env:
  CODEXCTL_ALLOWED_USERS: ${{ vars.CODEXCTL_ALLOWED_USERS }}
  CODEXCTL_GH_USERNAME: ${{ vars.CODEXCTL_GH_USERNAME }}
  CODEXCTL_GH_EMAIL:    ${{ vars.CODEXCTL_GH_EMAIL }}
concurrency:
  group: ai-plan-${{ github.event.issue.number }}
  cancel-in-progress: false

jobs:
  create-ai-plan:
    if: >-
      github.event.label.name == '[ai-plan]' &&
      contains(format(',{0},', vars.CODEXCTL_ALLOWED_USERS), format(',{0},', github.actor))
    name: "Allocate plan slot 🧩"
    runs-on: self-hosted
    timeout-minutes: 360
    environment: staging
    outputs:
      slot: ${{ steps.alloc.outputs.slot }}
      namespace: ${{ steps.alloc.outputs.namespace }}
    steps:
      - name: "Checkout project-example 📥"
        uses: actions/checkout@v4
        with:
          token: ${{ secrets.CODEXCTL_GH_PAT }}

      - name: "Allocate slot via codexctl 🧩"
        id: alloc
        env:
          GITHUB_RUN_ID:           ${{ github.run_id }}
          CODEXCTL_GH_PAT:         ${{ secrets.CODEXCTL_GH_PAT }}
          CODEXCTL_GH_USERNAME: ${{ vars.CODEXCTL_GH_USERNAME }}
          CODEXCTL_GH_EMAIL:    ${{ vars.CODEXCTL_GH_EMAIL }}
          CODEXCTL_ENV:            ai
          CODEXCTL_ISSUE_NUMBER:   ${{ github.event.issue.number }}
          CODEXCTL_DEV_SLOTS_MAX:  ${{ vars.CODEXCTL_DEV_SLOTS_MAX }}
        run: |
          set -euo pipefail
          codexctl ci ensure-slot

  deploy-ai-plan:
    needs: [create-ai-plan]
    name: "Deploy AI plan env 🚀"
    runs-on: self-hosted
    environment: staging
    outputs:
      infra_ready: ${{ steps.ensure.outputs.infra_ready }}
      infra_unhealthy: ${{ steps.ensure.outputs.infra_unhealthy }}
      codexctl_run_args: ${{ steps.ensure.outputs.codexctl_run_args }}
    steps:
      - name: "Checkout project-example 📥"
        uses: actions/checkout@v4
        with:
          ref: ${{ github.sha }}
          token: ${{ secrets.CODEXCTL_GH_PAT }}

      - name: "Apply AI plan env via codexctl 🚀"
        id: ensure
        env:
          GITHUB_RUN_ID:           ${{ github.run_id }}
          CODEXCTL_GH_PAT:         ${{ secrets.CODEXCTL_GH_PAT }}
          CODEXCTL_GH_USERNAME: ${{ vars.CODEXCTL_GH_USERNAME }}
          CODEXCTL_GH_EMAIL:    ${{ vars.CODEXCTL_GH_EMAIL }}
          CODEXCTL_ENV:            ai
          CODEXCTL_SLOT:           ${{ needs.create-ai-plan.outputs.slot }}
          CODEXCTL_ISSUE_NUMBER:   ${{ github.event.issue.number }}
          CODEXCTL_DEV_SLOTS_MAX:  ${{ vars.CODEXCTL_DEV_SLOTS_MAX }}
          CODEXCTL_CODE_ROOT_BASE: ${{ vars.CODEXCTL_CODE_ROOT_BASE }}
          CODEXCTL_DATA_ROOT:      ${{ vars.CODEXCTL_DATA_ROOT }}
          CODEXCTL_SOURCE:         .
          CODEXCTL_PREPARE_IMAGES: true
          CODEXCTL_APPLY:          true
          CODEXCTL_FORCE_APPLY:    true
          CODEXCTL_WAIT_SOFT_FAIL: true
          OPENAI_API_KEY:          ${{ secrets.OPENAI_API_KEY }}
          CONTEXT7_API_KEY:        ${{ secrets.CONTEXT7_API_KEY }}
          POSTGRES_USER:           ${{ secrets.POSTGRES_USER }}
          POSTGRES_PASSWORD:       ${{ secrets.POSTGRES_PASSWORD }}
          REDIS_PASSWORD:          ${{ secrets.REDIS_PASSWORD }}
          SECRET_KEY:              ${{ secrets.SECRET_KEY }}
        run: |
          set -euo pipefail
          export CODEXCTL_WORKSPACE_UID="$(id -u)"
          export CODEXCTL_WORKSPACE_GID="$(id -g)"
          codexctl ci ensure-ready

  run-codex-plan:
    needs: [create-ai-plan, deploy-ai-plan]
    name: "Run planning agent 🤖"
    runs-on: self-hosted
    environment: staging
    env:
      CODEXCTL_CODE_ROOT_BASE: ${{ vars.CODEXCTL_CODE_ROOT_BASE }}
      CODEXCTL_GH_PAT:         ${{ secrets.CODEXCTL_GH_PAT }}
    steps:
      - name: "Checkout default branch 📥"
        uses: actions/checkout@v4
        with:
          token: ${{ secrets.CODEXCTL_GH_PAT }}

      - name: "Run planning agent inline 🤖"
        env:
          GITHUB_RUN_ID:           ${{ github.run_id }}
          CODEXCTL_GH_PAT:         ${{ secrets.CODEXCTL_GH_PAT }}
          CODEXCTL_GH_USERNAME: ${{ vars.CODEXCTL_GH_USERNAME }}
          CODEXCTL_GH_EMAIL:    ${{ vars.CODEXCTL_GH_EMAIL }}
          CODEXCTL_ENV:            ai
          CODEXCTL_SLOT:           ${{ needs.create-ai-plan.outputs.slot }}
          CODEXCTL_ISSUE_NUMBER:   ${{ github.event.issue.number }}
          CODEXCTL_NAMESPACE:      ${{ needs.create-ai-plan.outputs.namespace }}
          CODEXCTL_LANG:    ru
          CODEXCTL_INFRA_UNHEALTHY: ${{ needs.deploy-ai-plan.outputs.infra_unhealthy }}
          OPENAI_API_KEY:          ${{ secrets.OPENAI_API_KEY }}
          CONTEXT7_API_KEY:        ${{ secrets.CONTEXT7_API_KEY }}
        run: |
          set -euo pipefail
          codexctl prompt run --kind plan_issue

  cleanup-ai-plan:
    needs: [create-ai-plan, deploy-ai-plan, run-codex-plan]
    if: always()
    name: "Cleanup plan env on failure 🧹"
    runs-on: self-hosted
    environment: staging
    env:
      CODEXCTL_GH_PAT:   ${{ secrets.CODEXCTL_GH_PAT }}
      CODEXCTL_DATA_ROOT: ${{ vars.CODEXCTL_DATA_ROOT }}
    steps:
      - name: "Checkout minimal 📥"
        uses: actions/checkout@v4
        with:
          token: ${{ secrets.CODEXCTL_GH_PAT }}

      - name: "Cleanup AI plan slot on failure (global) 🧹"
        env:
          CODEXCTL_ENV:          ai
          CODEXCTL_ISSUE_NUMBER: ${{ github.event.issue.number }}
          CODEXCTL_WITH_CONFIGMAP: true
        run: |
          set -euo pipefail

          STATUS_CREATE="${{ needs.create-ai-plan.result }}"
          STATUS_DEPLOY="${{ needs.deploy-ai-plan.result }}"
          STATUS_RUN="${{ needs.run-codex-plan.result }}"

          if [ "${STATUS_CREATE}" = "success" ] && [ "${STATUS_DEPLOY}" = "success" ] && [ "${STATUS_RUN}" = "success" ]; then
            echo "info: primary AI Plan workflow completed successfully, no cleanup required" >&2
            exit 0
          fi

          codexctl manage-env cleanup || true
```

### 👁 7.3. AI Plan Review (review planning results via comments)

Trigger: a new comment in an Issue (not a PR) that contains `[ai-plan]`. The workflow does:

1) `codexctl plan resolve-root` — find the root planning Issue for the current one (subtask/epic).
2) `ci ensure-ready` — bring up the environment (if not already up), with `CODEXCTL_ISSUE_NUMBER=<ROOT>`.
3) `prompt run --kind plan_review` with `CODEXCTL_FOCUS_ISSUE_NUMBER=<...>` — focus the agent on a specific task/comment.

```yaml
name: "AI Plan Review 👁"

on:
  issue_comment:
    types: [created]

env:
  CODEXCTL_ALLOWED_USERS: ${{ vars.CODEXCTL_ALLOWED_USERS }}
  CODEXCTL_GH_USERNAME: ${{ vars.CODEXCTL_GH_USERNAME }}
  CODEXCTL_GH_EMAIL:    ${{ vars.CODEXCTL_GH_EMAIL }}
concurrency:
  group: ai-plan-review-${{ github.event.issue.number }}
  cancel-in-progress: false

jobs:
  run:
    name: "Planning review agent run 🤖"
    if: >
      github.event.issue.pull_request == null &&
      contains(github.event.comment.body, '[ai-plan]') &&
      contains(format(',{0},', vars.CODEXCTL_ALLOWED_USERS), format(',{0},', github.actor))
    runs-on: self-hosted
    environment: staging
    env:
      CODEXCTL_CODE_ROOT_BASE:       ${{ vars.CODEXCTL_CODE_ROOT_BASE }}
      CODEXCTL_GH_PAT:         ${{ secrets.CODEXCTL_GH_PAT }}
      CODEXCTL_DATA_ROOT:            ${{ vars.CODEXCTL_DATA_ROOT }}
      GITHUB_RUN_ID:        ${{ github.run_id }}
      CODEXCTL_GH_USERNAME: ${{ vars.CODEXCTL_GH_USERNAME }}
      CODEXCTL_GH_EMAIL:    ${{ vars.CODEXCTL_GH_EMAIL }}
      OPENAI_API_KEY:       ${{ secrets.OPENAI_API_KEY }}
      CONTEXT7_API_KEY:     ${{ secrets.CONTEXT7_API_KEY }}
      POSTGRES_USER:        ${{ secrets.POSTGRES_USER }}
      POSTGRES_PASSWORD:    ${{ secrets.POSTGRES_PASSWORD }}
      REDIS_PASSWORD:       ${{ secrets.REDIS_PASSWORD }}
      SECRET_KEY:           ${{ secrets.SECRET_KEY }}
    steps:
      - name: "Checkout project-example 📥"
        uses: actions/checkout@v4
        with:
          token: ${{ secrets.CODEXCTL_GH_PAT }}
          persist-credentials: true
          fetch-depth: 1

      - name: "Resolve root planning issue 🔗"
        id: root_issue
        env:
          CODEXCTL_ISSUE_NUMBER: ${{ github.event.issue.number }}
          CODEXCTL_REPO:         ${{ github.repository }}
          CODEXCTL_GH_PAT:       ${{ secrets.CODEXCTL_GH_PAT }}
        run: |
          set -euo pipefail
          codexctl plan resolve-root

      - name: "Validate root issue 🧪"
        env:
          CODEXCTL_ROOT_ISSUE_NUMBER:  ${{ steps.root_issue.outputs.root }}
          CODEXCTL_FOCUS_ISSUE_NUMBER: ${{ steps.root_issue.outputs.focus }}
        run: |
          set -euo pipefail
          if [ -z "${CODEXCTL_ROOT_ISSUE_NUMBER}" ] || [ "${CODEXCTL_ROOT_ISSUE_NUMBER}" = "0" ]; then
            echo "error: unable to determine root planning issue for focus issue ${CODEXCTL_FOCUS_ISSUE_NUMBER}" >&2
            exit 1
          fi

      - name: "Resolve slot and namespace for root issue 📇"
        id: card
        env:
          CODEXCTL_ENV:            ai
          CODEXCTL_ISSUE_NUMBER:   ${{ steps.root_issue.outputs.root }}
          CODEXCTL_DEV_SLOTS_MAX:  ${{ vars.CODEXCTL_DEV_SLOTS_MAX }}
          CODEXCTL_SOURCE:         .
          CODEXCTL_PREPARE_IMAGES: true
          CODEXCTL_APPLY:          true
        run: |
          set -euo pipefail
          echo "info: ensuring AI planning environment ready via codexctl (ensure-ready)" >&2
          export CODEXCTL_WORKSPACE_UID="$(id -u)"
          export CODEXCTL_WORKSPACE_GID="$(id -g)"
          codexctl ci ensure-ready

      - name: "Run planning review agent via codexctl 🤖"
        env:
          GITHUB_RUN_ID:           ${{ github.run_id }}
          CODEXCTL_GH_PAT:         ${{ secrets.CODEXCTL_GH_PAT }}
          CODEXCTL_GH_USERNAME: ${{ vars.CODEXCTL_GH_USERNAME }}
          CODEXCTL_GH_EMAIL:    ${{ vars.CODEXCTL_GH_EMAIL }}
          CODEXCTL_ENV:            ai
          CODEXCTL_SLOT:           ${{ steps.card.outputs.slot }}
          CODEXCTL_NAMESPACE:      ${{ steps.card.outputs.namespace }}
          CODEXCTL_ISSUE_NUMBER:   ${{ steps.root_issue.outputs.root }}
          CODEXCTL_FOCUS_ISSUE_NUMBER: ${{ steps.root_issue.outputs.focus }}
          CODEXCTL_LANG:    ru
          CODEXCTL_PROMPT_CONTINUATION: ${{ steps.card.outputs.codexctl_new_env == 'true' && 'true' || 'false' }}
          CODEXCTL_RESUME:         ${{ steps.card.outputs.codexctl_new_env == 'true' && 'false' || 'true' }}
          OPENAI_API_KEY:          ${{ secrets.OPENAI_API_KEY }}
          CONTEXT7_API_KEY:        ${{ secrets.CONTEXT7_API_KEY }}
        run: |
          set -euo pipefail
          codexctl prompt run --kind plan_review
```
### 🛠 7.4. AI Dev by Issue (label `[ai-dev]`)

Workflow:

1) Check that the label is `[ai-dev]` and the actor is in `CODEXCTL_ALLOWED_USERS`.
2) `ci ensure-slot` — select/create a slot (values come from `CODEXCTL_ENV=ai`, `CODEXCTL_ISSUE_NUMBER=<N>`,
   `CODEXCTL_DEV_SLOTS_MAX`).
3) `ci ensure-ready` — bring up the AI-dev environment (`CODEXCTL_ENV=ai`, `CODEXCTL_SLOT=<SLOT>`,
   `CODEXCTL_ISSUE_NUMBER=<N>`, `CODEXCTL_PREPARE_IMAGES=true`, `CODEXCTL_APPLY=true`).
4) Prepare a working branch in the slot workspace (`codex/issue-<N>`).
5) `prompt run --kind dev_issue` — run the dev agent (if infra is unhealthy, set `CODEXCTL_INFRA_UNHEALTHY=true`).
6) auto-commit → push, find the PR by branch, attach the PR to the slot (`manage-env set`) and post a comment with links
   (`manage-env comment-pr`).
7) On failure — cleanup (`manage-env cleanup` with `CODEXCTL_WITH_CONFIGMAP=true`).

```yaml
name: "AI Dev Issue 🛠"

on:
  issues:
    types: [labeled]

env:
  CODEXCTL_ALLOWED_USERS: ${{ vars.CODEXCTL_ALLOWED_USERS }}
  CODEXCTL_GH_USERNAME: ${{ vars.CODEXCTL_GH_USERNAME }}
  CODEXCTL_GH_EMAIL:    ${{ vars.CODEXCTL_GH_EMAIL }}
concurrency:
  group: ai-issue-${{ github.event.issue.number }}
  cancel-in-progress: false

jobs:
  create-ai:
    name: "Allocate slot 🧩"
    if: >-
      github.event.label.name == '[ai-dev]' &&
      contains(format(',{0},', vars.CODEXCTL_ALLOWED_USERS), format(',{0},', github.actor))
    runs-on: self-hosted
    timeout-minutes: 360
    environment: staging
    outputs:
      slot: ${{ steps.alloc.outputs.slot }}
      namespace: ${{ steps.alloc.outputs.namespace }}
    steps:
      - name: "Checkout project-example 📥"
        uses: actions/checkout@v4
        with:
          token: ${{ secrets.CODEXCTL_GH_PAT }}

      - name: "Allocate slot via codexctl 🧩"
        id: alloc
        env:
          GITHUB_RUN_ID:           ${{ github.run_id }}
          CODEXCTL_GH_PAT:         ${{ secrets.CODEXCTL_GH_PAT }}
          CODEXCTL_GH_USERNAME: ${{ vars.CODEXCTL_GH_USERNAME }}
          CODEXCTL_GH_EMAIL:    ${{ vars.CODEXCTL_GH_EMAIL }}
          CODEXCTL_ENV:            ai
          CODEXCTL_ISSUE_NUMBER:   ${{ github.event.issue.number }}
          CODEXCTL_DEV_SLOTS_MAX:  ${{ vars.CODEXCTL_DEV_SLOTS_MAX }}
        run: |
          set -euo pipefail
          codexctl ci ensure-slot

  deploy-ai:
    needs: [create-ai]
    name: "Deploy AI environment 🚀"
    runs-on: self-hosted
    environment: staging
    outputs:
      infra_ready: ${{ steps.ensure.outputs.infra_ready }}
      infra_unhealthy: ${{ steps.ensure.outputs.infra_unhealthy }}
      codexctl_run_args: ${{ steps.ensure.outputs.codexctl_run_args }}
    steps:
      - name: "Checkout project-example 📥"
        uses: actions/checkout@v4
        with:
          ref: ${{ github.sha }}
          token: ${{ secrets.CODEXCTL_GH_PAT }}

      - name: "Ensure AI env ready via codexctl 🚀"
        id: ensure
        env:
          GITHUB_RUN_ID:           ${{ github.run_id }}
          CODEXCTL_GH_PAT:         ${{ secrets.CODEXCTL_GH_PAT }}
          CODEXCTL_GH_USERNAME: ${{ vars.CODEXCTL_GH_USERNAME }}
          CODEXCTL_GH_EMAIL:    ${{ vars.CODEXCTL_GH_EMAIL }}
          CODEXCTL_ENV:            ai
          CODEXCTL_SLOT:           ${{ needs.create-ai.outputs.slot }}
          CODEXCTL_ISSUE_NUMBER:   ${{ github.event.issue.number }}
          CODEXCTL_DEV_SLOTS_MAX:  ${{ vars.CODEXCTL_DEV_SLOTS_MAX }}
          CODEXCTL_CODE_ROOT_BASE: ${{ vars.CODEXCTL_CODE_ROOT_BASE }}
          CODEXCTL_DATA_ROOT:      ${{ vars.CODEXCTL_DATA_ROOT }}
          CODEXCTL_SOURCE:         .
          CODEXCTL_PREPARE_IMAGES: true
          CODEXCTL_APPLY:          true
          CODEXCTL_FORCE_APPLY:    true
          CODEXCTL_WAIT_SOFT_FAIL: true
          OPENAI_API_KEY:          ${{ secrets.OPENAI_API_KEY }}
          CONTEXT7_API_KEY:        ${{ secrets.CONTEXT7_API_KEY }}
          POSTGRES_USER:           ${{ secrets.POSTGRES_USER }}
          POSTGRES_PASSWORD:       ${{ secrets.POSTGRES_PASSWORD }}
          REDIS_PASSWORD:          ${{ secrets.REDIS_PASSWORD }}
          SECRET_KEY:              ${{ secrets.SECRET_KEY }}
        run: |
          set -euo pipefail
          export CODEXCTL_WORKSPACE_UID="$(id -u)"
          export CODEXCTL_WORKSPACE_GID="$(id -g)"
          codexctl ci ensure-ready

  run-codex:
    needs: [create-ai, deploy-ai]
    name: "Run dev agent 🤖"
    runs-on: self-hosted
    environment: staging
    env:
      CODEXCTL_CODE_ROOT_BASE: ${{ vars.CODEXCTL_CODE_ROOT_BASE }}
      CODEXCTL_GH_PAT:   ${{ secrets.CODEXCTL_GH_PAT }}
    steps:
      - name: "Checkout default branch 📥"
        uses: actions/checkout@v4
        with:
          token: ${{ secrets.CODEXCTL_GH_PAT }}

      - name: "Ensure working branch 🌿"
        env:
          CODEXCTL_SLOT: ${{ needs.create-ai.outputs.slot }}
          CODEXCTL_ISSUE_NUMBER: ${{ github.event.issue.number }}
        run: |
          set -euo pipefail
          cd "${CODEXCTL_CODE_ROOT_BASE}/${CODEXCTL_SLOT}/src"
          git config user.name "${CODEXCTL_GH_USERNAME}"
          git config user.email "${CODEXCTL_GH_EMAIL}"
          git checkout -b "codex/issue-${CODEXCTL_ISSUE_NUMBER}" || git checkout "codex/issue-${CODEXCTL_ISSUE_NUMBER}"
        shell: bash

      - name: "Run Codex dev agent 🤖"
        env:
          GITHUB_RUN_ID:           ${{ github.run_id }}
          CODEXCTL_GH_PAT:         ${{ secrets.CODEXCTL_GH_PAT }}
          CODEXCTL_GH_USERNAME: ${{ vars.CODEXCTL_GH_USERNAME }}
          CODEXCTL_GH_EMAIL:    ${{ vars.CODEXCTL_GH_EMAIL }}
          CODEXCTL_ENV:            ai
          CODEXCTL_SLOT:           ${{ needs.create-ai.outputs.slot }}
          CODEXCTL_ISSUE_NUMBER:   ${{ github.event.issue.number }}
          CODEXCTL_NAMESPACE:      ${{ needs.create-ai.outputs.namespace }}
          CODEXCTL_LANG:    ru
          CODEXCTL_INFRA_UNHEALTHY: ${{ needs.deploy-ai.outputs.infra_unhealthy }}
          OPENAI_API_KEY:          ${{ secrets.OPENAI_API_KEY }}
          CONTEXT7_API_KEY:        ${{ secrets.CONTEXT7_API_KEY }}
        run: |
          set -euo pipefail
          codexctl prompt run --kind dev_issue

      - name: "Auto-commit and push changes 📤"
        env:
          CODEXCTL_SLOT:        ${{ needs.create-ai.outputs.slot }}
          CODEXCTL_ISSUE_NUMBER: ${{ github.event.issue.number }}
        run: |
          set -euo pipefail
          cd "${CODEXCTL_CODE_ROOT_BASE}/${CODEXCTL_SLOT}/src"

          BRANCH="codex/issue-${CODEXCTL_ISSUE_NUMBER}"
          if git rev-parse --verify "$BRANCH" >/dev/null 2>&1; then
            git checkout "$BRANCH"
          fi

          rm -rf .bin || true

          git add -u
          git add docs proto services libs || true

          if git diff --cached --quiet; then
            echo "no changes to commit"
            exit 0
          fi

          MSG="feat: apply Codex changes for issue #${CODEXCTL_ISSUE_NUMBER}"
          git commit -m "$MSG"
          git push origin "$BRANCH"

      - name: "Detect PR for issue branch 🔎"
        id: detect_pr
        env:
          CODEXCTL_SLOT:         ${{ needs.create-ai.outputs.slot }}
          CODEXCTL_ISSUE_NUMBER: ${{ github.event.issue.number }}
          CODEXCTL_REPO:     ${{ github.repository }}
          CODEXCTL_GH_PAT:       ${{ secrets.CODEXCTL_GH_PAT }}
        run: |
          set -euo pipefail
          BRANCH="codex/issue-${CODEXCTL_ISSUE_NUMBER}"
          cd "${CODEXCTL_CODE_ROOT_BASE}/${CODEXCTL_SLOT}/src"

          printf '%s' "${CODEXCTL_GH_PAT}" | gh auth login --with-token >/dev/null 2>&1 || true

          PRN="$(gh pr list --head "${BRANCH}" --json number -q '.[0].number' 2>/dev/null || true)"
          if [ -z "${PRN}" ]; then
            echo "warn: PR not found for branch ${BRANCH}" >&2
            exit 0
          fi

          echo "codexctl_pr_number=${PRN}" >> "$GITHUB_OUTPUT"

      - name: "Attach PR number to slot 🏷️"
        if: steps.detect_pr.outputs.codexctl_pr_number != ''
        env:
          CODEXCTL_ENV:      ai
          CODEXCTL_SLOT:     ${{ needs.create-ai.outputs.slot }}
          CODEXCTL_PR_NUMBER: ${{ steps.detect_pr.outputs.codexctl_pr_number }}
        run: |
          set -euo pipefail
          codexctl manage-env set

      - name: "Comment to PR with env links 🔗"
        if: steps.detect_pr.outputs.codexctl_pr_number != ''
        env:
          CODEXCTL_ENV:       ai
          CODEXCTL_SLOT:      ${{ needs.create-ai.outputs.slot }}
          CODEXCTL_PR_NUMBER: ${{ steps.detect_pr.outputs.codexctl_pr_number }}
          CODEXCTL_LANG:      ru
          CODEXCTL_GH_PAT:    ${{ secrets.CODEXCTL_GH_PAT }}
        run: |
          set -euo pipefail
          codexctl manage-env comment-pr

  cleanup-ai:
    needs: [create-ai, deploy-ai, run-codex]
    if: always()
    name: "Cleanup on failure 🧹"
    runs-on: self-hosted
    environment: staging
    env:
      CODEXCTL_GH_PAT: ${{ secrets.CODEXCTL_GH_PAT }}
      CODEXCTL_DATA_ROOT: ${{ vars.CODEXCTL_DATA_ROOT }}
    steps:
      - name: "Checkout minimal 📥"
        uses: actions/checkout@v4
        with:
          token: ${{ secrets.CODEXCTL_GH_PAT }}

      - name: "Cleanup AI slot on failure (global) 🧹"
        env:
          CODEXCTL_ENV:          ai
          CODEXCTL_ISSUE_NUMBER: ${{ github.event.issue.number }}
          CODEXCTL_WITH_CONFIGMAP: true
        run: |
          set -euo pipefail

          STATUS_CREATE="${{ needs.create-ai.result }}"
          STATUS_DEPLOY="${{ needs.deploy-ai.result }}"
          STATUS_RUN="${{ needs.run-codex.result }}"

          if [ "${STATUS_CREATE}" = "success" ] && [ "${STATUS_DEPLOY}" = "success" ] && [ "${STATUS_RUN}" = "success" ]; then
            echo "info: primary AI Dev Issue workflow completed successfully, no cleanup required" >&2
            exit 0
          fi

          codexctl manage-env cleanup || true
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
  CODEXCTL_ALLOWED_USERS: ${{ vars.CODEXCTL_ALLOWED_USERS }}
  CODEXCTL_GH_USERNAME: ${{ vars.CODEXCTL_GH_USERNAME }}
  CODEXCTL_GH_EMAIL:    ${{ vars.CODEXCTL_GH_EMAIL }}
concurrency:
  group: ai-pr-${{ github.event.pull_request.number }}
  cancel-in-progress: false

jobs:
  run:
    name: "Review-fix agent run 🤖"
    if: >-
      github.event.review.state == 'changes_requested' &&
      contains(format(',{0},', vars.CODEXCTL_ALLOWED_USERS), format(',{0},', github.actor))
    runs-on: self-hosted
    environment: staging
    env:
      CODEXCTL_CODE_ROOT_BASE:       ${{ vars.CODEXCTL_CODE_ROOT_BASE }}
      CODEXCTL_GH_PAT:         ${{ secrets.CODEXCTL_GH_PAT }}
      CODEXCTL_DATA_ROOT:            ${{ vars.CODEXCTL_DATA_ROOT }}
      GITHUB_RUN_ID:        ${{ github.run_id }}
      CODEXCTL_GH_USERNAME: ${{ vars.CODEXCTL_GH_USERNAME }}
      CODEXCTL_GH_EMAIL:    ${{ vars.CODEXCTL_GH_EMAIL }}
      OPENAI_API_KEY:       ${{ secrets.OPENAI_API_KEY }}
      CONTEXT7_API_KEY:     ${{ secrets.CONTEXT7_API_KEY }}
      POSTGRES_USER:        ${{ secrets.POSTGRES_USER }}
      POSTGRES_PASSWORD:    ${{ secrets.POSTGRES_PASSWORD }}
      REDIS_PASSWORD:       ${{ secrets.REDIS_PASSWORD }}
      SECRET_KEY:           ${{ secrets.SECRET_KEY }}
    steps:
      - name: "Checkout PR head 📥"
        uses: actions/checkout@v4
        with:
          ref:   ${{ github.event.pull_request.head.ref }}
          token: ${{ secrets.CODEXCTL_GH_PAT }}
          fetch-depth: 0

      - name: "Ensure slot and namespace for PR 📇"
        id: card
        env:
          CODEXCTL_ENV:           ai
          CODEXCTL_PR_NUMBER:     ${{ github.event.pull_request.number }}
          CODEXCTL_DEV_SLOTS_MAX: ${{ vars.CODEXCTL_DEV_SLOTS_MAX }}
          CODEXCTL_SOURCE:        .
          CODEXCTL_PREPARE_IMAGES: true
          CODEXCTL_APPLY:          true
        run: |
          set -euo pipefail
          echo "info: ensuring AI PR review environment ready via codexctl (ensure-ready)" >&2
          export CODEXCTL_WORKSPACE_UID="$(id -u)"
          export CODEXCTL_WORKSPACE_GID="$(id -g)"
          codexctl ci ensure-ready

      - name: "Run Codex review-fix agent 🤖"
        env:
          GITHUB_RUN_ID:           ${{ github.run_id }}
          CODEXCTL_GH_PAT:         ${{ secrets.CODEXCTL_GH_PAT }}
          CODEXCTL_GH_USERNAME: ${{ vars.CODEXCTL_GH_USERNAME }}
          CODEXCTL_GH_EMAIL:    ${{ vars.CODEXCTL_GH_EMAIL }}
          CODEXCTL_ENV:            ai
          CODEXCTL_SLOT:           ${{ steps.card.outputs.slot }}
          CODEXCTL_NAMESPACE:      ${{ steps.card.outputs.namespace }}
          CODEXCTL_PR_NUMBER:      ${{ github.event.pull_request.number }}
          CODEXCTL_LANG:    ru
          CODEXCTL_PROMPT_CONTINUATION: ${{ steps.card.outputs.codexctl_new_env == 'true' && 'true' || 'false' }}
          CODEXCTL_RESUME:         ${{ steps.card.outputs.codexctl_new_env == 'true' && 'false' || 'true' }}
          OPENAI_API_KEY:          ${{ secrets.OPENAI_API_KEY }}
          CONTEXT7_API_KEY:        ${{ secrets.CONTEXT7_API_KEY }}
        run: |
          set -euo pipefail
          codexctl prompt run --kind dev_review

      - name: "Apply review changes and comment 💾"
        env:
          CODEXCTL_ENV:         ai
          CODEXCTL_SLOT:        ${{ steps.card.outputs.slot }}
          CODEXCTL_PR_NUMBER:   ${{ github.event.pull_request.number }}
          CODEXCTL_LANG:        ru
          CODEXCTL_REPO:    ${{ github.repository }}
          CODEXCTL_GH_PAT:      ${{ secrets.CODEXCTL_GH_PAT }}
        run: |
          set -euo pipefail
          codexctl pr review-apply
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

env:
  CODEXCTL_ALLOWED_USERS: ${{ vars.CODEXCTL_ALLOWED_USERS }}
  CODEXCTL_GH_USERNAME: ${{ vars.CODEXCTL_GH_USERNAME }}
  CODEXCTL_GH_EMAIL:    ${{ vars.CODEXCTL_GH_EMAIL }}
concurrency:
  group: ai-repair-${{ github.event.issue.number }}
  cancel-in-progress: false

jobs:
  create-ai-repair:
    name: "Allocate slot 🧩"
    if: >-
      github.event.label.name == '[ai-repair]' &&
      contains(format(',{0},', vars.CODEXCTL_ALLOWED_USERS), format(',{0},', github.actor))
    runs-on: self-hosted
    timeout-minutes: 360
    environment: staging
    outputs:
      slot: ${{ steps.alloc.outputs.slot }}
      namespace: ${{ steps.alloc.outputs.namespace }}
    steps:
      - name: "Checkout project-example 📥"
        uses: actions/checkout@v4
        with:
          token: ${{ secrets.CODEXCTL_GH_PAT }}

      - name: "Allocate slot via codexctl 🧩"
        id: alloc
        env:
          GITHUB_RUN_ID:           ${{ github.run_id }}
          CODEXCTL_GH_PAT:         ${{ secrets.CODEXCTL_GH_PAT }}
          CODEXCTL_GH_USERNAME: ${{ vars.CODEXCTL_GH_USERNAME }}
          CODEXCTL_GH_EMAIL:    ${{ vars.CODEXCTL_GH_EMAIL }}
          CODEXCTL_ENV:            ai-repair
          CODEXCTL_ISSUE_NUMBER:   ${{ github.event.issue.number }}
          CODEXCTL_DEV_SLOTS_MAX:  ${{ vars.CODEXCTL_DEV_SLOTS_MAX }}
        run: |
          set -euo pipefail
          codexctl ci ensure-slot

  deploy-ai-repair:
    needs: [create-ai-repair]
    name: "Deploy staging repair env 🚀"
    runs-on: self-hosted
    environment: staging
    steps:
      - name: "Checkout project-example 📥"
        uses: actions/checkout@v4
        with:
          ref: ${{ github.sha }}
          token: ${{ secrets.CODEXCTL_GH_PAT }}

      - name: "Sync staging sources 📂"
        env:
          CODEXCTL_CODE_ROOT_BASE: ${{ vars.CODEXCTL_CODE_ROOT_BASE }}
        run: |
          set -euo pipefail
          codexctl ci sync-sources

      - name: "Ensure staging repair env via codexctl 🚀"
        env:
          GITHUB_RUN_ID:           ${{ github.run_id }}
          CODEXCTL_GH_PAT:         ${{ secrets.CODEXCTL_GH_PAT }}
          CODEXCTL_GH_USERNAME: ${{ vars.CODEXCTL_GH_USERNAME }}
          CODEXCTL_GH_EMAIL:    ${{ vars.CODEXCTL_GH_EMAIL }}
          CODEXCTL_ENV:            ai-repair
          CODEXCTL_SLOT:           ${{ needs.create-ai-repair.outputs.slot }}
          CODEXCTL_PREFLIGHT:      true
          CODEXCTL_WAIT:           true
          CODEXCTL_ONLY_INFRA:     namespace-and-config,codex-ai-repair-rbac
          CODEXCTL_ONLY_SERVICES:  codex
          OPENAI_API_KEY:          ${{ secrets.OPENAI_API_KEY }}
          CONTEXT7_API_KEY:        ${{ secrets.CONTEXT7_API_KEY }}
          CODEXCTL_CODE_ROOT_BASE: ${{ vars.CODEXCTL_CODE_ROOT_BASE }}
          CODEXCTL_DATA_ROOT:      ${{ vars.CODEXCTL_DATA_ROOT }}
          POSTGRES_USER:           ${{ secrets.POSTGRES_USER }}
          POSTGRES_PASSWORD:       ${{ secrets.POSTGRES_PASSWORD }}
          REDIS_PASSWORD:          ${{ secrets.REDIS_PASSWORD }}
          SECRET_KEY:              ${{ secrets.SECRET_KEY }}
        run: |
          set -euo pipefail
          export CODEXCTL_WORKSPACE_UID="$(id -u)"
          export CODEXCTL_WORKSPACE_GID="$(id -g)"
          codexctl ci apply

      - name: "Cleanup staging repair env on failure 🧹"
        if: failure() || cancelled()
        env:
          CODEXCTL_ENV:  ai-repair
          CODEXCTL_SLOT: ${{ needs.create-ai-repair.outputs.slot }}
          CODEXCTL_WITH_CONFIGMAP: true
        run: |
          set -euo pipefail
          codexctl manage-env cleanup || true

  run-codex:
    needs: [create-ai-repair, deploy-ai-repair]
    name: "Run staging repair agent 🤖"
    runs-on: self-hosted
    environment: staging
    env:
      CODEXCTL_CODE_ROOT_BASE: ${{ vars.CODEXCTL_CODE_ROOT_BASE }}
      CODEXCTL_GH_PAT:   ${{ secrets.CODEXCTL_GH_PAT }}
    steps:
      - name: "Checkout default branch 📥"
        uses: actions/checkout@v4
        with:
          token: ${{ secrets.CODEXCTL_GH_PAT }}

      - name: "Sync staging sources 📂"
        env:
          CODEXCTL_CODE_ROOT_BASE: ${{ vars.CODEXCTL_CODE_ROOT_BASE }}
        run: |
          set -euo pipefail
          codexctl ci sync-sources

      - name: "Ensure working branch 🌿"
        env:
          CODEXCTL_ISSUE_NUMBER: ${{ github.event.issue.number }}
        run: |
          set -euo pipefail
          WORKDIR="${CODEXCTL_CODE_ROOT_BASE}/staging/src"
          cd "${WORKDIR}"
          git config user.name "${CODEXCTL_GH_USERNAME}"
          git config user.email "${CODEXCTL_GH_EMAIL}"
          git checkout -b "codex/ai-repair-${CODEXCTL_ISSUE_NUMBER}" || git checkout "codex/ai-repair-${CODEXCTL_ISSUE_NUMBER}"
        shell: bash

      - name: "Run Codex staging repair agent 🤖"
        env:
          GITHUB_RUN_ID:           ${{ github.run_id }}
          CODEXCTL_GH_PAT:         ${{ secrets.CODEXCTL_GH_PAT }}
          CODEXCTL_GH_USERNAME: ${{ vars.CODEXCTL_GH_USERNAME }}
          CODEXCTL_GH_EMAIL:    ${{ vars.CODEXCTL_GH_EMAIL }}
          CODEXCTL_ENV:            ai-repair
          CODEXCTL_SLOT:           ${{ needs.create-ai-repair.outputs.slot }}
          CODEXCTL_NAMESPACE:      ${{ needs.create-ai-repair.outputs.namespace }}
          CODEXCTL_ISSUE_NUMBER:   ${{ github.event.issue.number }}
          CODEXCTL_LANG:    ru
          OPENAI_API_KEY:          ${{ secrets.OPENAI_API_KEY }}
          CONTEXT7_API_KEY:        ${{ secrets.CONTEXT7_API_KEY }}
        run: |
          set -euo pipefail
          if [ -z "${CODEXCTL_SLOT}" ] || [ "${CODEXCTL_SLOT}" = "0" ]; then
            echo "error: CODEXCTL_SLOT is empty or 0" >&2
            exit 1
          fi
          codexctl prompt run --kind ai-repair_issue

      - name: "Cleanup staging repair env on failure 🧹"
        if: failure() || cancelled()
        env:
          CODEXCTL_ENV:  ai-repair
          CODEXCTL_SLOT: ${{ needs.create-ai-repair.outputs.slot }}
          CODEXCTL_WITH_CONFIGMAP: true
        run: |
          set -euo pipefail
          if [ -z "${CODEXCTL_SLOT}" ]; then
            exit 0
          fi
          codexctl manage-env cleanup || true

      - name: "Auto-commit and push changes 📤"
        env:
          CODEXCTL_ISSUE_NUMBER: ${{ github.event.issue.number }}
        run: |
          set -euo pipefail
          WORKDIR="${CODEXCTL_CODE_ROOT_BASE}/staging/src"
          cd "${WORKDIR}"

          BRANCH="codex/ai-repair-${CODEXCTL_ISSUE_NUMBER}"
          if git rev-parse --verify "$BRANCH" >/dev/null 2>&1; then
            git checkout "$BRANCH"
          fi

          rm -rf .bin || true

          git add -u
          git add docs proto services libs || true

          if git diff --cached --quiet; then
            echo "no changes to commit"
            exit 0
          fi

          MSG="fix: staging repair for issue #${CODEXCTL_ISSUE_NUMBER}"
          git commit -m "$MSG"
          git push origin "$BRANCH"

      - name: "Detect PR for issue branch 🔎"
        id: detect_pr
        env:
          CODEXCTL_ISSUE_NUMBER: ${{ github.event.issue.number }}
          CODEXCTL_REPO: ${{ github.repository }}
          CODEXCTL_GH_PAT: ${{ secrets.CODEXCTL_GH_PAT }}
        run: |
          set -euo pipefail
          BRANCH="codex/ai-repair-${CODEXCTL_ISSUE_NUMBER}"
          WORKDIR="${CODEXCTL_CODE_ROOT_BASE}/staging/src"
          cd "${WORKDIR}"

          printf '%s' "${CODEXCTL_GH_PAT}" | gh auth login --with-token >/dev/null 2>&1 || true

          PRN="$(gh pr list --head "${BRANCH}" --json number -q '.[0].number' 2>/dev/null || true)"
          if [ -z "${PRN}" ]; then
            echo "warn: PR not found for branch ${BRANCH}" >&2
            exit 0
          fi

          echo "codexctl_pr_number=${PRN}" >> "$GITHUB_OUTPUT"

      - name: "Attach PR number to slot 🏷️"
        if: steps.detect_pr.outputs.codexctl_pr_number != ''
        env:
          CODEXCTL_ENV:      ai-repair
          CODEXCTL_SLOT:     ${{ needs.create-ai-repair.outputs.slot }}
          CODEXCTL_PR_NUMBER: ${{ steps.detect_pr.outputs.codexctl_pr_number }}
        run: |
          set -euo pipefail
          codexctl manage-env set

      - name: "Comment to PR with env links 🔗"
        if: steps.detect_pr.outputs.codexctl_pr_number != ''
        env:
          CODEXCTL_ENV:       ai-repair
          CODEXCTL_SLOT:      ${{ needs.create-ai-repair.outputs.slot }}
          CODEXCTL_PR_NUMBER: ${{ steps.detect_pr.outputs.codexctl_pr_number }}
          CODEXCTL_LANG:      ru
          CODEXCTL_GH_PAT:    ${{ secrets.CODEXCTL_GH_PAT }}
        run: |
          set -euo pipefail
          codexctl manage-env comment-pr || true
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

env:
  CODEXCTL_ALLOWED_USERS: ${{ vars.CODEXCTL_ALLOWED_USERS }}
  CODEXCTL_GH_USERNAME: ${{ vars.CODEXCTL_GH_USERNAME }}
  CODEXCTL_GH_EMAIL:    ${{ vars.CODEXCTL_GH_EMAIL }}
concurrency:
  group: ai-repair-pr-${{ github.event.pull_request.number }}
  cancel-in-progress: false

jobs:
  run:
    name: "Staging repair review run 🤖"
    if: >-
      github.event.review.state == 'changes_requested' &&
      startsWith(github.event.pull_request.head.ref, 'codex/ai-repair-') &&
      contains(format(',{0},', vars.CODEXCTL_ALLOWED_USERS), format(',{0},', github.actor))
    runs-on: self-hosted
    environment: staging
    env:
      CODEXCTL_CODE_ROOT_BASE:       ${{ vars.CODEXCTL_CODE_ROOT_BASE }}
      CODEXCTL_GH_PAT:         ${{ secrets.CODEXCTL_GH_PAT }}
      CODEXCTL_DATA_ROOT:            ${{ vars.CODEXCTL_DATA_ROOT }}
      GITHUB_RUN_ID:        ${{ github.run_id }}
      CODEXCTL_GH_USERNAME: ${{ vars.CODEXCTL_GH_USERNAME }}
      CODEXCTL_GH_EMAIL:    ${{ vars.CODEXCTL_GH_EMAIL }}
      KUBECONFIG:           /home/runner/.kube/microk8s.config
      OPENAI_API_KEY:       ${{ secrets.OPENAI_API_KEY }}
      CONTEXT7_API_KEY:     ${{ secrets.CONTEXT7_API_KEY }}
      POSTGRES_USER:        ${{ secrets.POSTGRES_USER }}
      POSTGRES_PASSWORD:    ${{ secrets.POSTGRES_PASSWORD }}
      REDIS_PASSWORD:       ${{ secrets.REDIS_PASSWORD }}
      SECRET_KEY:           ${{ secrets.SECRET_KEY }}
    steps:
      - name: "Checkout PR head 📥"
        uses: actions/checkout@v4
        with:
          ref:   ${{ github.event.pull_request.head.ref }}
          token: ${{ secrets.CODEXCTL_GH_PAT }}
          fetch-depth: 0

      - name: "Sync staging sources 📂"
        env:
          CODEXCTL_CODE_ROOT_BASE: ${{ vars.CODEXCTL_CODE_ROOT_BASE }}
        run: |
          set -euo pipefail
          if [ -z "${CODEXCTL_CODE_ROOT_BASE:-}" ]; then
          codexctl ci sync-sources

      - name: "Resolve slot and namespace for PR 📇"
        id: card
        env:
          CODEXCTL_ENV:           ai-repair
          CODEXCTL_PR_NUMBER:     ${{ github.event.pull_request.number }}
          CODEXCTL_DEV_SLOTS_MAX: ${{ vars.CODEXCTL_DEV_SLOTS_MAX }}
        run: |
          set -euo pipefail
          codexctl ci ensure-ready

      - name: "Ensure staging repair env via codexctl 🚀"
        env:
          GITHUB_RUN_ID:           ${{ github.run_id }}
          CODEXCTL_GH_PAT:         ${{ secrets.CODEXCTL_GH_PAT }}
          CODEXCTL_GH_USERNAME: ${{ vars.CODEXCTL_GH_USERNAME }}
          CODEXCTL_GH_EMAIL:    ${{ vars.CODEXCTL_GH_EMAIL }}
          CODEXCTL_ENV:            ai-repair
          CODEXCTL_SLOT:           ${{ steps.card.outputs.slot }}
          CODEXCTL_PREFLIGHT:      true
          CODEXCTL_WAIT:           true
          CODEXCTL_ONLY_INFRA:     namespace-and-config,codex-ai-repair-rbac
          CODEXCTL_ONLY_SERVICES:  codex
          OPENAI_API_KEY:          ${{ secrets.OPENAI_API_KEY }}
          CONTEXT7_API_KEY:        ${{ secrets.CONTEXT7_API_KEY }}
          CODEXCTL_CODE_ROOT_BASE: ${{ vars.CODEXCTL_CODE_ROOT_BASE }}
          CODEXCTL_DATA_ROOT:      ${{ vars.CODEXCTL_DATA_ROOT }}
          POSTGRES_USER:           ${{ secrets.POSTGRES_USER }}
          POSTGRES_PASSWORD:       ${{ secrets.POSTGRES_PASSWORD }}
          REDIS_PASSWORD:          ${{ secrets.REDIS_PASSWORD }}
          SECRET_KEY:              ${{ secrets.SECRET_KEY }}
        run: |
          set -euo pipefail
          export CODEXCTL_WORKSPACE_UID="$(id -u)"
          export CODEXCTL_WORKSPACE_GID="$(id -g)"
          codexctl ci apply

      - name: "Run Codex staging repair review 🤖"
        env:
          GITHUB_RUN_ID:           ${{ github.run_id }}
          CODEXCTL_GH_PAT:         ${{ secrets.CODEXCTL_GH_PAT }}
          CODEXCTL_GH_USERNAME: ${{ vars.CODEXCTL_GH_USERNAME }}
          CODEXCTL_GH_EMAIL:    ${{ vars.CODEXCTL_GH_EMAIL }}
          CODEXCTL_ENV:            ai-repair
          CODEXCTL_SLOT:           ${{ steps.card.outputs.slot }}
          CODEXCTL_NAMESPACE:      ${{ steps.card.outputs.namespace }}
          CODEXCTL_PR_NUMBER:      ${{ github.event.pull_request.number }}
          CODEXCTL_LANG:    ru
          CODEXCTL_PROMPT_CONTINUATION: ${{ (steps.card.outputs.codexctl_new_env == 'true' || steps.card.outputs.codexctl_env_ready != 'true') && 'true' || 'false' }}
          CODEXCTL_RESUME:         ${{ (steps.card.outputs.codexctl_new_env == 'true' || steps.card.outputs.codexctl_env_ready != 'true') && 'false' || 'true' }}
          OPENAI_API_KEY:          ${{ secrets.OPENAI_API_KEY }}
          CONTEXT7_API_KEY:        ${{ secrets.CONTEXT7_API_KEY }}
        run: |
          set -euo pipefail
          codexctl prompt run --kind ai-repair_review

      - name: "Apply review changes and comment 💾"
        env:
          CODEXCTL_ENV:       ai-repair
          CODEXCTL_SLOT:      ${{ steps.card.outputs.slot }}
          CODEXCTL_PR_NUMBER: ${{ github.event.pull_request.number }}
          CODEXCTL_LANG:      ru
          CODEXCTL_REPO:  ${{ github.repository }}
          CODEXCTL_GH_PAT:    ${{ secrets.CODEXCTL_GH_PAT }}
        run: |
          set -euo pipefail
          codexctl pr review-apply

      - name: "Cleanup staging repair env on failure 🧹"
        if: (failure() || cancelled()) && steps.card.outputs.slot != ''
        env:
          CODEXCTL_ENV:  ai-repair
          CODEXCTL_SLOT: ${{ steps.card.outputs.slot }}
          CODEXCTL_WITH_CONFIGMAP: true
        run: |
          set -euo pipefail
          codexctl manage-env cleanup || true
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
          token: ${{ secrets.CODEXCTL_GH_PAT }}
      - if: github.event_name == 'pull_request'
        env:
          CODEXCTL_PR_NUMBER: ${{ github.event.pull_request.number }}
          CODEXCTL_BRANCH: ${{ github.event.pull_request.head.ref }}
          CODEXCTL_REPO: ${{ github.repository }}
          CODEXCTL_WITH_CONFIGMAP: true
          CODEXCTL_DELETE_BRANCH: true
        run: codexctl manage-env cleanup-pr || true
      - if: github.event_name == 'pull_request' && github.event.pull_request.merged == true
        env:
          CODEXCTL_BRANCH: ${{ github.event.pull_request.head.ref }}
          CODEXCTL_REPO: ${{ github.repository }}
          CODEXCTL_GH_PAT: ${{ secrets.CODEXCTL_GH_PAT }}
          CODEXCTL_CLOSE_ISSUE: true
        run: codexctl manage-env close-linked-issue || true
      - if: github.event_name == 'issues'
        env:
          CODEXCTL_ISSUE_NUMBER: ${{ github.event.issue.number }}
          CODEXCTL_REPO: ${{ github.repository }}
          CODEXCTL_WITH_CONFIGMAP: true
          CODEXCTL_DELETE_BRANCH: true
        run: codexctl manage-env cleanup-issue || true
```

Full example: `project-example` repo, `.github/workflows/ai_cleanup.yml`.

### 🔑 7.9. Secrets and PAT for the GitHub bot

Recommended set of secrets/vars in your project repository (e.g. `codex-project`):

- `CODEXCTL_GH_PAT` — PAT for a GitHub bot user;
- `CODEXCTL_GH_USERNAME` — bot username. Do not use a developer’s personal account; create a dedicated technical account.
- `KUBECONFIG` / paths to kubeconfig for staging;
- secrets for DB/Redis/cache/queue (username/password, DSN, etc.);
- `REGISTRY_HOST` and (optionally) registry credentials.
- `OPENAI_API_KEY` — OpenAI API key.
- `CONTEXT7_API_KEY` — Context7 API key (if used).
- `CODEXCTL_ALLOWED_USERS` (vars) — list of allowed GitHub users, in the format `user1,user2,user3`.
- `CODEXCTL_DEV_SLOTS_MAX` (vars) — maximum number of slots that `ci ensure-slot/ensure-ready` can allocate.

How to create a user and PAT:

1. Create a separate technical GitHub account for the bot (e.g. `codex-bot-42`).
2. In account settings, open **Developer settings → Personal access tokens → Fine-grained**.
3. Create a token with permissions:
   - access to the project repository (e.g. `codex-project`, read/write for `code`, `pull requests`, `issues`);
   - access to Actions (if you need to manage workflows).
4. Save the token and add it to the repository secrets as `CODEXCTL_GH_PAT`.

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
