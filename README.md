# codexctl

`codexctl` is a portable Go CLI for managing Kubernetes stacks using a declarative `services.yaml` description.

The tool is designed to be project‑agnostic: all project‑specific details (namespaces, domains, image tags, dev‑slot patterns) live in configuration and GitHub workflows, not in the core code.

## Goals (high level)

- single CLI for `render`, `apply`, `destroy`, `status`;
- management of dev/AI ephemeral environments and slots (`manage-env`);
- prompt rendering for AI agents (`prompt render`);
- environment preflight checks (`doctor`);
- reusable GitHub workflows for deploy and AI environments.

## Status

The project is functional but still evolving:

- Go module and layered package layout are in place;
- core commands (`render`, `apply`, `destroy`, `status`, `manage-env`, `prompt`, `doctor`) are implemented for real use;
- logging is based on `log/slog` with a color handler (`tint`).

You can build the binary and inspect the help:

```bash
go build ./cmd/codexctl
./codexctl --help
```

## Minimal `services.yaml` example

Below is a simplified example of a `services.yaml` file for a generic AI‑driven web project. It demonstrates the core ideas (project‑level config, environments, infrastructure groups, services) without aiming to be production‑complete.

```yaml
project: my-ai-project

envFiles:
  - VERSIONS

namespace:
  patterns:
    dev:      "{{ .Project }}-dev"
    staging:  "{{ .Project }}-staging"
    ai:       "{{ .Project }}-dev-{{ .Slot }}"

maxSlots: 0

registry: '{{ envOr "REGISTRY_HOST" "localhost:32000" }}'

baseDomain:
  dev:     "dev.my-ai-project.com"
  staging: "staging.my-ai-project.com"
  ai:      "staging.my-ai-project.com"

environments:
  dev:
    kubeconfig: "~/.kube/my-ai-project-work"
    context:    "my-ai-project-work"
  staging:
    kubeconfig: "~/.kube/my-ai-project-staging.yaml"
    context:    "my-ai-project-staging"
  ai:
    from: "staging"

infrastructure:
  - name: ingress-nginx
    manifests:
      - path: deploy/ingress-nginx.controller.yaml

  - name: core-config
    manifests:
      - path: deploy/configmap.yaml
      - path: deploy/secret.yaml

services:
  - name: django-backend
    manifests:
      - path: services/django_backend/deploy.yaml
    image:
      repository: '{{ envOr "REGISTRY_HOST" "localhost:32000" }}/my-ai-project/django-backend'
      tagTemplate: '{{ printf "%s-%s" .Env (index .Versions "django-backend") }}'
    overlays:
      dev:
        hostMounts:
          - name: go-src
            hostPath: "{{ .ProjectRoot }}/services/django_backend"
            mountPath: "/app"

  - name: user-gateway
    manifests:
      - path: services/user_gateway/deploy.yaml
    image:
      repository: '{{ envOr "REGISTRY_HOST" "localhost:32000" }}/my-ai-project/user-gateway'
      tagTemplate: '{{ printf "%s-%s" .Env (index .Versions "user-gateway") }}'

codex:
  # Path to a Codex config template (e.g. TOML) relative to the project root.
  # It is rendered with the same template context as services.yaml and can be materialized via:
  #   codexctl prompt config --env ai --slot 1 --out ~/.codex/config.toml
  configTemplate: "deploy/codex/config.toml"
```

The actual `services.yaml` schema will evolve, but even this small sample illustrates the direction:

- behavior is described declaratively in configuration;
- adding a new service does not require Go code changes;
- each project is free to define its own namespaces, domains, image tagging strategies and slot patterns.
