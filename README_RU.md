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

Проект находится на ранней стадии scaffolding:

- создан Go‑модуль и структура пакетов;
- настроен CLI на базе Cobra с подкомандами, но сами команды пока только сообщают, что они не реализованы;
- логирование построено на `log/slog` с цветным handler’ом (`tint`).

Уже сейчас можно собрать бинарь и посмотреть справку:

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

registry: "{{ .EnvMap.REGISTRY_HOST | default \"localhost:32000\" }}"

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
      repository: "{{ .registry }}/my-ai-project/django-backend"
      tagTemplate: "{{ printf \"%s-%s\" .Env (index .EnvMap \"DJANGO_BACKEND_VERSION\") }}"
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
      repository: "{{ .registry }}/my-ai-project/user-gateway"
      tagTemplate: "{{ printf \"%s-%s\" .Env (index .EnvMap \"USER_GATEWAY_VERSION\") }}"
```

Фактическая схема `services.yaml` будет эволюционировать, но даже этот небольшой пример показывает направление:

- поведение описывается декларативно в конфигурации;
- добавление нового сервиса не требует правок Go‑кода;
- каждый проект свободен выбирать свои паттерны namespace’ов, доменов, стратегий тегирования образов и dev‑слотов.
