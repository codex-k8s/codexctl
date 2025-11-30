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

images:
  busybox:
    type: external
    from: 'docker.io/library/busybox:1.37.0'
    local: '{{ envOr "REGISTRY_HOST" "localhost:32000" }}/library/busybox:1.37.0'

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

The runner should have `go` and `kubectl` installed, as well as access configured to the required Kubernetes clusters.

### Image storage with MicroK8s

When running against a MicroK8s cluster, a practical setup is:

  - Enable the built-in container registry on the cluster node:

  ```bash
  microk8s enable registry
  microk8s status --wait-ready
  ```

  - Configure your project to push images to that registry (which listens on `localhost:32000` by default), for example:

  ```yaml
  registry: '{{ envOr "REGISTRY_HOST" "localhost:32000" }}'

  services:
    - name: my-service
      image:
        repository: '{{ envOr "REGISTRY_HOST" "localhost:32000" }}/my-project/my-service'
  ```

  - Ensure the runner can pull base images from Docker Hub without rate limits:

  ```bash
  docker login docker.io
  ```

  The stored credentials will be used when `docker build` pulls large upstream images (e.g. Python, Go, Node base images) during hooks defined in `services.yaml`.

 In this model `codexctl` builds images locally (using Docker) and pushes them to the MicroK8s registry endpoint configured via `REGISTRY_HOST` / `registry` in `services.yaml`.

- Configure Docker on the runner to treat the MicroK8s registry as an insecure registry (so that `docker pull/push localhost:32000/...` works without TLS errors). On a typical Linux host this can be done via `/etc/docker/daemon.json`:

  ```json
  {
    "insecure-registries": ["localhost:32000"]
  }
  ```

  After changing the file, restart the Docker daemon (for example, `sudo systemctl restart docker`). This is safe in this context because the registry is only reachable locally on the CI runner.

To pre-populate the local registry with external base images (declared in the `images` block with `type: external`), you can run:

```bash
codexctl images mirror --env staging
```

This command reads `services.yaml`, and for each external image ensures that the `local` reference exists by pulling `from` and pushing it to the local registry if needed.***

### CLI logging and debug mode

`codexctl` uses a structured logger with levels. The default level is `info`, but you can override it via:

```bash
export CODEX_LOG_LEVEL=debug   # or info|warn|error
```

When set to `debug`, additional details are logged, such as previews of the rendered Codex config and prompt before they are uploaded into the Codex pod. At non-debug levels these `Debug` calls are suppressed by the logger configuration.***
