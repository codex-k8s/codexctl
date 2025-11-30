# codexctl

`codexctl` — переносимая CLI‑утилита на Go для управления Kubernetes‑стеками по декларативному описанию в `services.yaml`.

Инструмент спроектирован как проект‑агностичный: все специфичные детали (namespace’ы, домены, теги образов, паттерны dev‑слотов) задаются в конфигурации и GitHub‑воркфлоу, а не зашиваются в код.

## Цели (высокий уровень)

- единый CLI для `render`, `apply`, `destroy`, `status`;
- управление dev/AI ephemeral‑окружениями и слотами (`manage-env`);
- рендер промптов для AI‑агентов (`prompt render`);
- preflight‑проверки окружения (`doctor`);
- reusable GitHub‑воркфлоу для деплоя и AI‑окружений.

## Статус

Проект уже пригоден к использованию, но активная доработка продолжается:

- создан Go‑модуль и слоистая структура пакетов;
- реализованы основные команды (`render`, `apply`, `destroy`, `status`, `manage-env`, `prompt`, `doctor`);
- логирование построено на `log/slog` с цветным handler’ом (`tint`).

Можно собрать бинарь и посмотреть справку:

```bash
go build ./cmd/codexctl
./codexctl --help
```

## Минимальный пример `services.yaml`

Ниже приведён упрощённый пример файла `services.yaml` для условного AI‑ориентированного web‑проекта. Он показывает ключевые идеи (проектная конфигурация, окружения, инфраструктурные группы, сервисы), не претендуя на полноту продакшен‑кейса.

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
  # Путь до шаблона конфигурации Codex (например TOML) относительно корня проекта.
  # Рендерится в том же контексте, что и services.yaml, и может быть материализован командой:
  #   codexctl prompt config --env ai --slot 1 --out ~/.codex/config.toml
  configTemplate: "deploy/codex/config.toml"
```

Фактическая схема `services.yaml` будет эволюционировать, но даже этот небольшой пример показывает направление:

- поведение описывается декларативно в конфигурации;
- добавление нового сервиса не требует правок Go‑кода;
- каждый проект свободен выбирать свои паттерны namespace’ов, доменов, стратегий тегирования образов и dev‑слотов.

На раннере должен быть установлен `go` и `kubectl`, а также настроен доступ к нужным Kubernetes‑кластерам.

### Хранилище образов в MicroK8s

Если кластер поднят на MicroK8s, удобно использовать встроенный registry:

- Включите registry на узле кластера:

  ```bash
  microk8s enable registry
  microk8s status --wait-ready
  ```

- Настройте проект на использование этого registry (по умолчанию он слушает `localhost:32000`):

  ```yaml
  registry: '{{ envOr "REGISTRY_HOST" "localhost:32000" }}'

  services:
    - name: my-service
      image:
        repository: '{{ envOr "REGISTRY_HOST" "localhost:32000" }}/my-project/my-service'
  ```

- Залогиньтесь в Docker Hub на раннере, чтобы не упираться в лимиты при скачивании базовых образов:

  ```bash
  docker login docker.io
  ```

  Эти креденшелы будут использованы при `docker build`, когда в hook’ах `services.yaml` подтягиваются большие базовые образы (Python, Go, Node и т.п.).

В такой конфигурации `codexctl` собирает образы локально (через Docker) и пушит их в registry MicroK8s, заданный через `REGISTRY_HOST` / `registry` в `services.yaml`.

- Настройте Docker на раннере так, чтобы registry MicroK8s считался «insecure registry» (чтобы `docker pull/push localhost:32000/...` не падали из‑за TLS). Обычно это делается через `/etc/docker/daemon.json`:

  ```json
  {
    "insecure-registries": ["localhost:32000"]
  }
  ```

  После правки перезапустите демон Docker (например, `sudo systemctl restart docker`). В нашем случае это безопасно, так как registry доступен только локально на CI‑раннере.

Чтобы заранее залить во встроенный registry внешние базовые образы (описанные в блоке `images` с `type: external`), можно вызвать:

```bash
codexctl images mirror --env staging
```

Команда читает `services.yaml` и для каждого внешнего образа проверяет наличие `local`‑тэга в registry; если его нет — делает `docker pull` из `from` и `docker push` в локальный реестр.***

### Логирование CLI и режим debug

`codexctl` использует структурированный логгер с уровнями. По умолчанию уровень — `info`, но его можно переопределить:

```bash
export CODEX_LOG_LEVEL=debug   # или info|warn|error
```

При уровне `debug` логгер дополнительно выводит, например, превью отрендеренного конфига Codex и промпта перед загрузкой их в pod Codex. На других уровнях (`info`/`warn`/`error`) эти `Debug`‑сообщения автоматически отфильтровываются.***
