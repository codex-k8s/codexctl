<div align="center">
  <img src="docs/media/logo.png" alt="PAI logo" width="120" height="120" />
  <h1>codexctl</h1>
  <p>🧠 Инструмент для управления облачными процессами планирования и разработки в Kubernetes‑кластере через ИИ‑агентов на основе <a href="https://github.com/openai/codex">codex-cli от OpenAI</a> и GitHub‑workflow.</p>
</div>

![Go Version](https://img.shields.io/github/go-mod/go-version/codex-k8s/codexctl)
[![Go Reference](https://pkg.go.dev/badge/github.com/codex-k8s/codexctl.svg)](https://pkg.go.dev/github.com/codex-k8s/codexctl)

🇬🇧 English version: [README_EN.md](README_EN.md)

`codexctl` — это CLI‑инструмент для декларативного управления Kubernetes‑окружениями и AI-dev слотами на базе одного
файла конфигурации `services.yaml`. Он упрощает:

- развёртывание инфраструктуры (БД, кэши, ingress, observability) и приложений в Kubernetes‑проектах;
- подготовку временных AI-dev окружений под задачи/PR, в которых работает Codex‑агент;
- рендеринг манифестов и конфигов (включая конфиг Codex) с использованием шаблонов.

По сути, это «оркестратор поверх `kubectl` и шаблонов», который знает про:

- окружения (`dev`, `ai-staging`, `ai`, `ai-repair`);
- слоты (`ai`‑окружения для задач/PR);
- структуру проекта (инфраструктура, сервисы, Pod Codex‑агента).

> Важно: утилита находится на ранней стадии разработки, см. раздел «Безопасность и стабильность» в конце.

## 🎯 Цель и идеальный DX для AI‑агента

`codexctl` задуман как «кнопка» для облачной ИИ‑разработки и планирования в Kubernetes: по Issue/PR поднимается изолированное
окружение (namespace/слот) с тем же стеком, что у проекта (сервисы, БД, кэши, очереди, ingress, observability), а агент
работает *внутри* кластера рядом с этим стеком.

Это даёт практический опыт «как у живого разработчика», но без локальной установки всего окружения:

- агент делает HTTP‑запросы к сервисам в кластере, проверяет поведение и контракты;
- смотрит логи/события, метрики, статус rollout’ов;
- подключается к PostgreSQL/Redis/очередям, проверяет миграции и данные;
- применяет инфраструктуру/сервисы декларативно через `services.yaml` и `codexctl apply/ci apply`.

Рабочий пример (готовые `services.yaml` и GitHub Actions workflow’ы): https://github.com/codex-k8s/project-example

---

## 📦 Установка

Требования к локальному Go‑toolchain:

- Go **>= 1.25.1** (см. `go.mod`).

Инструкцию по подготовке VPS/self-hosted runner (microk8s, kubectl, gh, kaniko и т.д.) см. в:
https://github.com/codex-k8s/project-example/blob/main/README.md

`codexctl` распространяется как Go‑CLI. При установленном Go‑toolchain его можно поставить командой:

```bash
go install github.com/codex-k8s/codexctl/cmd/codexctl@latest
```

Либо установить конкретную версию (подставив актуальный SemVer‑тег вместо `v42.42.42`):

```bash
go install github.com/codex-k8s/codexctl/cmd/codexctl@v42.42.42
```

Документация Go‑пакетов доступна на pkg.go.dev: https://pkg.go.dev/github.com/codex-k8s/codexctl.

---

## 🚨 Важно: зависимости от внешних бинарников

Сейчас `codexctl` **зависит от внешних CLI‑утилит** и запускает их как подпроцессы. Это осознанно упрощает старт и
интеграцию с существующими практиками (kubectl/gh/git/kaniko), но требует, чтобы эти бинарники были установлены и доступны
в `PATH` (как на self-hosted runner, так и в контейнере с Codex).

Минимально необходимые утилиты:

- `kubectl` — применение/удаление манифестов, `wait`, диагностика (см. `internal/kube/*`, `hooks: kubectl.wait`);
- `bash` — выполнение hook‑шагов `run:` (см. `internal/hooks/*`);
- `kaniko` — сборка/зеркалирование образов (`images mirror/build`, см. `internal/cli/images.go`);
- `git` — commit/push в PR‑флоу (см. `internal/cli/pr.go`);
- `gh` — чтение/комментирование Issues/PR и GraphQL/REST вызовы (см. `internal/githubapi/*`, `internal/cli/*`).

Проверка окружения: используйте `codexctl doctor` (он проверяет наличие `kubectl`, `bash`, `git`, `gh`, а также `kaniko`
при наличии блока `images` в `services.yaml`).

План на будущее: постепенно заменять часть внешних зависимостей на встроенные реализации (клиенты Kubernetes/GitHub/OCI,
логика синхронизации и т.п.) через соответствующие SDK/библиотеки, чтобы уменьшить набор обязательных бинарников и сделать
запуски более предсказуемыми.

Практическую инструкцию по установке необходимых утилит на VPS для runner’а см. в:
https://github.com/codex-k8s/project-example/blob/main/README.md

---

## 💡 1. Ключевые идеи

### 📦 1.1. Один `services.yaml` на весь проект

Вместо разрозненных Helm‑чартов и bash‑скриптов используется один файл `services.yaml`, в котором описано:

- какие образы использовать и как их собирать (`images`);
- какие инфраструктурные манифесты применять (`infrastructure`);
- какие сервисы разворачивать (`services`);
- как выглядят окружения (`environments`), пространства имён и слоты (`namespace`, `state`);
- как конфигурируется Pod Codex‑агента (`codex`).

Этот файл служит единым источником правды для `codexctl`, GitHub Actions и AI-dev окружений.

### 🧩 1.2. Шаблонизация и контекст

`services.yaml` и все подключаемые манифесты рендерятся через Go‑шаблоны. В шаблонах доступны:

- `{{ .Env }}` — текущее окружение (`dev`, `ai-staging`, `ai`, `ai-repair`);
- `{{ .Namespace }}` — Kubernetes namespace;
- `{{ .Project }}` — имя проекта (`codex-project`);
- `{{ .Slot }}` — номер слота для AI-dev окружения;
- `{{ .BaseDomain }}` — карта базовых доменов (`dev`, `ai-staging`, `ai`, `ai-repair`);
- `{{ .Versions }}` — карта версий сервисов/образов;
- функции `envOr`, `default`, `ternary`, `join` и т.д.

Этим же контекстом пользуются:

- рендер манифестов (`codexctl apply` / `codexctl ci apply`);
- встроенные шаблоны промптов (`internal/prompt/templates/*.tmpl`);
- шаблон конфига Codex (`internal/prompt/templates/config_default.toml` или переопределённый).

### 🌐 1.3. Окружения и слоты

`codexctl` работает с типами окружений:

- `dev` — локальное окружение разработчика (один namespace);
- `ai-staging` — стейджинг‑кластер (CI/CD, приближённый к продакшену);
- `ai` — AI-dev слоты: изолированные namespace’ы вида `<project>-dev-<slot>` (например, `codex-project-dev-<slot>`),
  с доменами `dev-<slot>.ai-staging.<domain>`, в которых работают Codex‑агенты над задачами/PR.
- `ai-repair` — Pod Codex в namespace ai-staging с RBAC только для нужных ресурсов (восстановление).

Слоты (`slot`) — это числовые идентификаторы AI-dev окружений, которыми управляет `codexctl ci ensure-slot/ensure-ready`. Для каждого
слота создаётся и поддерживается:

- отдельный namespace;
- отдельный набор PVC/данных (`.data/slots/<slot>` на хосте);
- отдельный Pod `codex` с образом агента и смонтированными исходниками вашего проекта (в примерах — `codex-project`).

---

### 🧪 1.4. Флоу по Issue и роль агента

Базовая задумка такова:

- вы создаёте Issue в репозитории и вешаете на него определённый лейбл, например `[ai-plan]` для планирования
  или `[ai-dev]` для разработки;
- GitHub Actions‑workflow реагирует на этот лейбл, вызывает `codexctl ci ensure-slot/ensure-ready`
  (все параметры берутся из переменных `CODEXCTL_*`) и разворачивает в отдельном namespace полный стек инфраструктуры и сервисов проекта;
- в этом namespace запускается Pod `codex` с Codex‑агентом, которому `codexctl prompt run` подсовывает промпт
  нужного типа (`kind=plan_issue` или `kind=dev_issue`, языки — `ru`/`en`).

Главная особенность подхода — агент работает **в живом окружении** и «дебажит» свои изменения так же, как это сделал бы
разработчик:

- читает логи сервисов через `kubectl logs`;
- ходит в БД и кеши (через `psql`, `redis-cli` или собственные CLI/HTTP/gRPC‑клиенты);
- выполняет реальные запросы к HTTP/gRPC‑эндпоинтам проекта;
- может запускать тесты, миграции, загружать фикстуры, перезапускать деплойменты.

При этом каждое AI-dev окружение изолировано (свой namespace и данные), поэтому агент не мешает другим разработчикам и
не трогает сервисы других разработчиков и агентов.

### 🏷️ 1.5. Лейблы Issue и как они влияют на инструкции агенту

В этом проекте есть два класса лейблов:

1) **Триггер‑лейблы (workflow‑лейблы)** — управляют тем, какой тип агента/сессии будет запущен:
- `[ai-plan]` — режим планирования (агент готовит план/Issue‑структуру, без PR и коммитов);
- `[ai-dev]` — режим разработки (агент вносит изменения в код, делает коммиты и открывает PR);
- `[ai-repair]` — режим восстановления/ремонта окружения (ai-staging/инфраструктура) и PR при необходимости.

> Важно: агент **не должен** сам добавлять триггер‑лейблы `[ai-dev]`, `[ai-plan]`, `[ai-repair]`, если пользователь явно этого не просил.

2) **Семантические лейблы задачи** — описывают тип работы и влияют на то, как агент формулирует план/действия.
Эти лейблы могут быть повешены вместе (несколько одновременно):
- `feature` — планирование/реализация новой функциональности (включая рефакторинг, новые сервисы и т.п.);
- `bug` — поиск причины и/или исправление ошибки/неверной логики;
- `doc` — написание/актуализация документации;
- `debt` — устранение техдолга (рефакторинг, обновление зависимостей, улучшение качества);
- `idea` — брейншторм/проработка идеи (несколько вариантов, вопросы, обсуждение в комментариях);
- `epic` — крупная задача‑эпик, разбитая на подзадачи.

3) **Лейблы конфигурации модели/рассуждений** — позволяют выбрать модель и степень рассуждений для агента
   (поддерживаются как на Issue, так и на PR; приоритет: флаги запуска → Issue → PR → переменные окружения → services.yaml → дефолты config.toml):
- модель: `[ai-model-gpt-5.2-codex]`, `[ai-model-gpt-5.2]`, `[ai-model-gpt-5.1-codex-max]`, `[ai-model-gpt-5.1-codex-mini]`;
- рассуждения: `[ai-reasoning-low]`, `[ai-reasoning-medium]`, `[ai-reasoning-high]`, `[ai-reasoning-extra-high]`.

Как это работает в инструкциях агенту:
- В режимах **планирования** (`[ai-plan]`) агент ориентируется на эти лейблы при структуре плана (feature/bug/doc/debt/idea) и может создавать новые Issues/эпики/подзадачи *только если пользователь просит такой формат*. Для связки дочерних задач используется маркер `AI-PLAN-PARENT: #<root>` в теле Issues.
- В режимах **разработки** (`[ai-dev]`) агент следует семантике лейблов (feature/bug/doc/debt) при реализации и проверках; при необходимости может создавать дополнительные Issues для найденных побочных задач (например, `bug`/`doc`/`debt`), не мешая основной задаче.
- В режимах **review результатов планирования** агент отвечает на комментарии пользователя и, если пользователь просит доработать результат, **редактирует существующий результат** (комментарий/Issue body), а не создаёт новый (если не попросили дополнительные варианты).

## 🚀 2. Быстрый старт

### ✅ 2.1. Требования

- Kubernetes‑кластер (отдельный от продакшена).
- Доступный `kubectl` и kubeconfig для выбранного окружения.
- Kaniko executor (по умолчанию `/kaniko/executor`) и кластерный registry (`CODEXCTL_REGISTRY_HOST`).
- Собранный бинарь `codexctl` в `PATH`.

### 📝 2.2. Минимальный `services.yaml` для проекта

Простейший пример (в актуальном формате; см. также `services.yaml` в репозитории https://github.com/codex-k8s/project-example):

```yaml
# {{- $workspaceMount := envOr "CODEXCTL_WORKSPACE_MOUNT" "/workspace" -}}
# {{- $codeRootBase := envOr "CODEXCTL_CODE_ROOT_BASE" (printf "%s/codex/envs" $workspaceMount) -}}
# {{- $codeRootRel := trimPrefix $codeRootBase (printf "%s/" $workspaceMount) -}}
# {{- $devCodeRoot := printf "%s/dev/src" $codeRootRel -}}
# {{- $slotCodeRoot := $codeRootRel -}}
# {{- $aiStagingCodeRoot := printf "%s/ai-staging/src" $codeRootRel -}}
# {{- $workspacePVC := envOr "CODEXCTL_WORKSPACE_PVC" (printf "%s-workspace" .Project) -}}
# {{- $registryHost := envOr "CODEXCTL_REGISTRY_HOST" (printf "registry.%s-ai-staging.svc.cluster.local:5000" .Project) -}}

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
    - Перед началом работы прочитай ./AGENTS.md и релевантную документацию в docs/*.md.
    - Для работы с манифестами используй `codexctl render` и `codexctl apply` только с фильтрами `--only-services/--only-infra` (или `--skip-*`).
  servicesOverview: |
    - Django backend: админка и миграции БД PostgreSQL.
    - Go chat backend: HTTP API чата, авторизация, работа с PostgreSQL и Redis.
    - Web frontend: SPA‑интерфейс чата.
  timeouts:
    exec: "60m"
    rollout: "30m"

baseDomain:
  dev: '{{ envOr "CODEXCTL_BASE_DOMAIN_DEV" "dev.example-domain.ru" }}'
  ai-staging: '{{ envOr "CODEXCTL_BASE_DOMAIN_AI_STAGING" "ai-staging.example-domain.ru" }}'
  ai: '{{ envOr "CODEXCTL_BASE_DOMAIN_AI" (envOr "CODEXCTL_BASE_DOMAIN_AI_STAGING" "ai-staging.example-domain.ru") }}'
  ai-repair: '{{ envOr "CODEXCTL_BASE_DOMAIN_AI_STAGING" "ai-staging.example-domain.ru" }}'

namespace:
  patterns:
    dev: "{{ .Project }}-dev"
    ai-staging: "{{ .Project }}-ai-staging"
    ai: "{{ .Project }}-dev-{{ .Slot }}"
    ai-repair: "{{ .Project }}-ai-staging"

registry: '{{ $registryHost }}'

storage:
  workspace:
    size: "50Gi"
    accessModes: ["ReadWriteMany"]
    storageClass: '{{ envOr "CODEXCTL_STORAGE_CLASS_WORKSPACE" "" }}'
  data:
    size: "20Gi"
    accessModes: ["ReadWriteOnce"]
    storageClass: '{{ envOr "CODEXCTL_STORAGE_CLASS_DATA" "" }}'

state:
  backend: configmap
  configmapNamespace: codex-system
  configmapPrefix: codex-env-

environments:
  dev:
    kubeconfig: "/home/user/.kube/project-example-dev"
    imagePullPolicy: IfNotPresent
  ai-staging:
    kubeconfig: "/home/runner/.kube/microk8s.config"
    imagePullPolicy: Always
  ai:
    from: "ai-staging"
    imagePullPolicy: IfNotPresent
  ai-repair:
    from: "ai-staging"
    imagePullPolicy: IfNotPresent

images:
  postgres:
    type: external
    from: "docker.io/library/postgres:16-bookworm"
    local: '{{ $registryHost }}/library/postgres:16-bookworm'
  # build‑образы сервисов описываются аналогично (dockerfile/context/buildArgs/tagTemplate)

infrastructure:
  - name: namespace-and-config
    when: '{{ or (eq .Env "dev") (eq .Env "ai-staging") (eq .Env "ai") }}'
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
        pvcMounts:
          - name: workspace
            claimName: '{{ $workspacePVC }}'
            mountPath: "/app"
            subPath: '{{ printf "%s/%d/src/services/chat_backend" $slotCodeRoot .Slot }}'
        dropKinds: ["Ingress"]
```

В реальном проекте блоки будут богаче (версии, hooks, overlays), но базовый принцип тот же.

### 🔁 2.3. Базовый цикл деплоя

Для любого окружения (`dev`, `ai-staging`, `ai`, `ai-repair`) цикл один и тот же:

```bash
export CODEXCTL_ENV=ai-staging   # или dev/ai
# для ai дополнительно задайте: CODEXCTL_SLOT=<slot>

codexctl images mirror    # при необходимости
codexctl images build     # сборка и пуш образов из images.type=build

# Рекомендуется применять только через фильтры (и отдельно infra/services).
codexctl apply --only-infra namespace-and-config,data-services,observability,cluster-dns,tls-issuer,echo-probe --wait --preflight
codexctl apply --only-services django-backend,chat-backend,web-frontend --wait
```

Имена групп инфраструктуры и сервисов берутся из `services.yaml` вашего проекта; в примерах используются значения из `project-example`.

При работе через GitHub Actions этот цикл зашит в workflow — см. раздел про интеграцию.

---

## 📑 3. Формат services.yaml

`services.yaml` — это «manifest of manifests» для вашего проекта. Ниже — обзор ключевых блоков.

### 🌱 3.1. Корневые поля

- `project` — код проекта, используется в namespace’ах и других шаблонах.
- `envFiles` — список `.env`‑файлов с переменными окружения, которые подключаются при рендере.
- `registry` — базовый адрес реестра (например, `registry.<project>-ai-staging.svc.cluster.local:5000`).
- `storage` — параметры PVC (workspace/data/registry).
- `versions` — словарь версий (произвольные ключи, используются в шаблонах).

### 🤖 3.2. Блок `codex`

Конфигурация интеграции с Codex‑агентом:

- `codex.configTemplate` — путь до шаблона конфига Codex (например, `deploy/codex/config.toml`). Если не указан,
  используется встроенный `internal/prompt/templates/config_default.toml`.
- `codex.links` — список ссылок (title + path), которые будут рендериться в комментариях к окружению (например, Swagger, Admin).
- `codex.extraTools` — список дополнительных CLI/утилит, доступных в образе агента и полезных для промптов
  (например, `psql`, `redis-cli`, `k6`).
- `codex.projectContext` — свободный текст с особенностями проекта (куда смотреть документацию, как запускать тесты и т.п.);
  вставляется в промпты (см. встроенные шаблоны).
- `codex.servicesOverview` — обзор инфраструктурных/прикладных сервисов и их URL/порты; также попадает в промпты.
- `codex.timeouts.exec`/`codex.timeouts.rollout` — таймауты для `prompt run` и ожидания rollout’ов.

Эти поля используются при рендере встроенных промптов (`dev_issue_*`, `plan_issue_*`, `plan_review_*`,
`dev_review_*`, `ai-repair_*`) и конфига Codex:

- `internal/prompt/templates/*.tmpl` — шаблоны промптов;
- `internal/prompt/templates/config_default.toml` — дефолтный конфиг Codex.

Вы можете переопределить:

- конфиг Codex через `codex.configTemplate`;
- сами промпты — указав свой `--template` для `codexctl prompt ...` или подменив встроенные `.tmpl` в образе.

### 🌐 3.3. `baseDomain` и `namespace`

```yaml
baseDomain:
  dev: "dev.codex-project.local"
  ai-staging: "ai-staging.codex-project.local"
  ai: "ai-staging.codex-project.local"
  ai-repair: "ai-staging.codex-project.local"

namespace:
  patterns:
    dev: "{{ .Project }}-dev"
    ai-staging: "{{ .Project }}-ai-staging"
    ai: "{{ .Project }}-dev-{{ .Slot }}"
    ai-repair: "{{ .Project }}-ai-staging"
```

- `baseDomain` — домены для ingress’ов по окружениям.
- `namespace.patterns` — шаблоны namespace’ов; для `ai` по умолчанию используется `project-dev-<slot>`.

### 🗺️ 3.4. `environments`

Описание подключения к кластерам:

```yaml
environments:
  dev:
    kubeconfig: "/home/user/.kube/project-example-dev"
    imagePullPolicy: IfNotPresent
  ai-staging:
    kubeconfig: "/home/runner/.kube/microk8s.config"
    imagePullPolicy: Always
  ai:
    from: "ai-staging"
    imagePullPolicy: IfNotPresent
  ai-repair:
    from: "ai-staging"
    imagePullPolicy: IfNotPresent
```

- `from` позволяет наследовать настройки (например, `ai` от `ai-staging`).
- реестр образов задаётся через корневое поле `registry` и `CODEXCTL_REGISTRY_HOST`.

### 🖼️ 3.5. `images`

Описывает внешние и собираемые образы:

```yaml
images:
  busybox:
    type: external
    from: 'docker.io/library/busybox:{{ index .Versions "busybox" }}'
    local: '{{ envOr "CODEXCTL_REGISTRY_HOST" (printf "registry.%s-ai-staging.svc.cluster.local:5000" .Project) }}/library/busybox:{{ index .Versions "busybox" }}'

  chat-backend:
    type: build
    repository: '{{ envOr "CODEXCTL_REGISTRY_HOST" (printf "registry.%s-ai-staging.svc.cluster.local:5000" .Project) }}/project-example/chat-backend'
    tagTemplate: '{{ printf "%s-%s" (ternary (eq .Env "ai") "ai-staging" .Env) (index .Versions "chat-backend") }}'
    dockerfile: 'services/chat_backend/Dockerfile'
    context: 'services/chat_backend'
    buildArgs:
      GOLANG_IMAGE_VERSION: '{{ index .Versions "golang" }}'
      SERVICE_VERSION: '{{ index .Versions "chat-backend" }}'
```

- `type: external` — образы, которые зеркалируются командой `images mirror`;
- `type: build` — образы, которые собираются и пушатся командой `images build`.

### 🏗️ 3.6. `infrastructure`

Список инфраструктурных сервисов:

```yaml
infrastructure:
  - name: namespace-and-config
    when: '{{ or (eq .Env "dev") (eq .Env "ai-staging") (eq .Env "ai") }}'
    manifests:
      - path: deploy/namespace.yaml
      - path: deploy/configmap.yaml
      - path: deploy/secret.yaml

  - name: data-services
    when: '{{ or (eq .Env "dev") (eq .Env "ai-staging") (eq .Env "ai") }}'
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

Каждый элемент:

- описывает набор YAML‑файлов (с шаблонами);
- может содержать `hooks.beforeApply/afterApply/afterDestroy` с вызовами `kubectl` или shell‑скриптов.

### 🧱 3.7. `services`

Список приложений:

```yaml
# {{- $workspaceMount := envOr "CODEXCTL_WORKSPACE_MOUNT" "/workspace" -}}
# {{- $codeRootBase := envOr "CODEXCTL_CODE_ROOT_BASE" (printf "%s/codex/envs" $workspaceMount) -}}
# {{- $codeRootRel := trimPrefix $codeRootBase (printf "%s/" $workspaceMount) -}}
# {{- $slotCodeRoot := $codeRootRel -}}
# {{- $aiStagingCodeRoot := printf "%s/ai-staging/src" $codeRootRel -}}
# {{- $workspacePVC := envOr "CODEXCTL_WORKSPACE_PVC" (printf "%s-workspace" .Project) -}}
# {{- $registryHost := envOr "CODEXCTL_REGISTRY_HOST" (printf "registry.%s-ai-staging.svc.cluster.local:5000" .Project) -}}

services:
  - name: chat-backend
    manifests:
      - path: services/chat_backend/deploy.yaml
    image:
      repository: '{{ $registryHost }}/project-example/chat-backend'
      tagTemplate: '{{ printf "%s-%s" (ternary (eq .Env "ai") "ai-staging" .Env) (index .Versions "chat-backend") }}'
    overlays:
      dev:
        pvcMounts:
          - name: workspace
            claimName: '{{ $workspacePVC }}'
            mountPath: "/app"
            subPath: '{{ printf "%s/services/chat_backend" $devCodeRoot }}'
      ai-staging:
        pvcMounts:
          - name: workspace
            claimName: '{{ $workspacePVC }}'
            mountPath: "/app"
            subPath: '{{ printf "%s/services/chat_backend" $aiStagingCodeRoot }}'
      ai:
        pvcMounts:
          - name: workspace
            claimName: '{{ $workspacePVC }}'
            mountPath: "/app"
            subPath: '{{ printf "%s/%d/src/services/chat_backend" $slotCodeRoot .Slot }}'
        dropKinds: ["Ingress"]
```

- `manifests` — список YAML‑файлов для сервиса;
- `image` — переопределение `image:` в манифестах (репозиторий/тэг);
- `overlays` — настройки по окружениям (PVC‑монтаж исходников, отключение ingress в AI-dev и т.п.).
- `pvcMounts` — список монтируемых путей из PVC (исходники для dev/AI-dev).
  Опционально: `subPath` для таргетной директории внутри PVC.
- `dropKinds` — список Kubernetes‑ресурсов (по kind), которые нужно выкинуть из рендера (например, Ingress в AI-dev).

---

## 🛠️ 4. Применение манифестов

### ☸️ 4.1. `codexctl apply`

```bash
# ai-staging (пример для project-example)
export CODEXCTL_ENV=ai-staging
codexctl apply \
  --only-infra namespace-and-config,data-services,observability,cluster-dns,tls-issuer,echo-probe \
  --wait --preflight

codexctl apply \
  --only-services django-backend,chat-backend,web-frontend \
  --wait

# AI-dev слот
export CODEXCTL_ENV=ai
export CODEXCTL_SLOT=123
codexctl apply \
  --only-services chat-backend \
  --wait --preflight
```

Команда:

- рендерит стэк;
- выполняет preflight‑проверки (если включены флагом `--preflight`);
- применяет манифесты через `kubectl apply`;
- выполняет хуки `afterApply` (например, ожидание rollout’ов);
- при `--wait` дожидается готовности деплойментов.

Фильтры для безопасного применения:

- `--only-services name1,name2` — применить только выбранные сервисы;
- `--skip-services name1,name2` — пропустить выбранные сервисы;
- `--only-infra name1,name2` — применить только выбранные группы инфраструктуры;
- `--skip-infra name1,name2` — пропустить выбранные группы инфраструктуры.

При запуске внутри Pod’а Codex всегда используйте фильтры и не применяйте сервис `codex`.
Дополнительно (часто важно именно внутри Pod’а Codex): используйте `--skip-infra tls-issuer,echo-probe`, чтобы не упираться
в cluster-scope ресурсы и проверки локальных портов (см. встроенные промпты `*_issue_*.tmpl`).

### 🧩 4.2. `codexctl render`

Рендер манифестов без применения:

```bash
export CODEXCTL_ENV=ai-staging
codexctl render \
  --only-services web-frontend
```

---

## ⌨️ 5. Команды codexctl: обзор

### ⚙️ 5.1. Глобальные флаги

Во всех командах значения можно задавать через `CODEXCTL_*`; флаги имеют приоритет.

- `CODEXCTL_CONFIG` / `--config, -c` — путь к `services.yaml` (по умолчанию `services.yaml` в текущем каталоге).
- `CODEXCTL_ENV` / `--env` — имя окружения (`dev`, `ai-staging`, `ai`, `ai-repair`).
- `CODEXCTL_NAMESPACE` / `--namespace` — явный override namespace (обычно не нужен).
- `CODEXCTL_LOG_LEVEL` / `--log-level` — уровень логов (`debug`, `info`, `warn`, `error`).

### ☸️ 5.2. `apply`

- Назначение: отрендерить и применить стэк в Kubernetes.
- Типичный пример — см. раздел 4.1.

### 🧩 5.3. `render`

- Назначение: отрендерить манифесты в stdout без применения.
- Удобно использовать в CI или внутри Pod’а Codex для проверки результата.

### 🧪 5.4. `ci`

Набор команд для CI‑сценариев и подготовки слотов.

Подкоманды:

- `ci images` — зеркалирует внешние образы и/или собирает локальные для CI.
  Параметры берутся из `CODEXCTL_*` (например, `CODEXCTL_MIRROR_IMAGES`, `CODEXCTL_BUILD_IMAGES`, `CODEXCTL_SLOT`, `CODEXCTL_VARS`, `CODEXCTL_VAR_FILE`).
- `ci apply` — применяет манифесты с ретраями и опциональным ожиданием.
  Параметры берутся из `CODEXCTL_*` (например, `CODEXCTL_PREFLIGHT`, `CODEXCTL_WAIT`, `CODEXCTL_APPLY_RETRIES`, `CODEXCTL_WAIT_RETRIES`,
  `CODEXCTL_APPLY_BACKOFF`, `CODEXCTL_WAIT_BACKOFF`, `CODEXCTL_WAIT_TIMEOUT`, `CODEXCTL_REQUEST_TIMEOUT`,
  фильтры рендера `CODEXCTL_ONLY_SERVICES/CODEXCTL_SKIP_SERVICES/CODEXCTL_ONLY_INFRA/CODEXCTL_SKIP_INFRA`).
- `ci sync-sources` — синхронизирует исходники в workspace.
  Параметры берутся из `CODEXCTL_*` (например, `CODEXCTL_CODE_ROOT_BASE`, `CODEXCTL_SOURCE`, `CODEXCTL_ENV`, `CODEXCTL_SLOT`).
- `ci ensure-slot` — выделяет/повторно использует слот по селектору `CODEXCTL_ISSUE_NUMBER`/`CODEXCTL_PR_NUMBER`/`CODEXCTL_SLOT` (один обязателен).
  При наличии `GITHUB_OUTPUT` пишет `slot`, `namespace`, `env` в outputs GitHub Actions.
- `ci ensure-ready` — гарантирует слот и при необходимости синхронизирует исходники, готовит образы и применяет манифесты.
  Параметры берутся из `CODEXCTL_*` (например, `CODEXCTL_CODE_ROOT_BASE`, `CODEXCTL_SOURCE`, `CODEXCTL_PREPARE_IMAGES`, `CODEXCTL_APPLY`,
  `CODEXCTL_FORCE_APPLY`, `CODEXCTL_WAIT_TIMEOUT`, `CODEXCTL_WAIT_SOFT_FAIL`). При наличии `GITHUB_OUTPUT` пишет `slot`, `namespace`, `env`,
  `created`, `recreated`, `infra_ready`, `codexctl_env_ready`, `infra_unhealthy`, `codexctl_new_env`, `codexctl_run_args` (булевы значения — `true/false`). При `CODEXCTL_CODE_ROOT_BASE` и `CODEXCTL_SOURCE` исходники синхронизируются в
  `<CODEXCTL_CODE_ROOT_BASE>/<slot>/src`.

### 🖼️ 5.5. `images`

Подкоманды:

- `images mirror` — зеркалирует `images.type=external` в локальный реестр:

  ```bash
  export CODEXCTL_ENV=ai-staging
  codexctl images mirror
  ```

- `images build` — собирает и пушит `images.type=build`:

  ```bash
  export CODEXCTL_ENV=ai-staging
  codexctl images build
  ```

### 🎛️ 5.6. `manage-env`

Группа команд для метаданных и очистки AI-dev слотов (`env=ai`):

- `manage-env cleanup` — удаляет окружение слота и записи состояния.
- `manage-env cleanup-pr` — чистит окружения по PR и (опционально) удаляет ветку/закрывает связанную Issue.
- `manage-env cleanup-issue` — чистит окружения по Issue и (опционально) удаляет ветки `codex/*`.
- `manage-env close-linked-issue` — закрывает Issue, определённую по имени ветки `codex/issue-*` или `codex/ai-repair-*`.
- `manage-env set` — проставить связи slot ↔ issue/PR.
- `manage-env comment` — рендерить ссылки на окружение для комментариев.
- `manage-env comment-pr` — рендерит и публикует комментарий со ссылками в PR.

Примечания:

- `manage-env cleanup` поддерживает `CODEXCTL_ALL` / `--all` (очистить все подходящие слоты) и
  `CODEXCTL_WITH_CONFIGMAP` / `--with-configmap` (удалить state‑ConfigMap у выбранных окружений).
- `manage-env comment` и `manage-env comment-pr` принимают `CODEXCTL_LANG` / `--lang en|ru` для языка комментария.

### 🧠 5.7. `prompt`

Команды работы с промптами Codex‑агента:

- `prompt run` — запуск Codex‑агента в Pod’е `codex`:

  ```bash
  export CODEXCTL_ENV=ai
  export CODEXCTL_SLOT=1
  export CODEXCTL_LANG=ru
  codexctl prompt run --kind dev_issue
  ```

  Использует встроенные шаблоны промптов (`internal/prompt/templates/dev_issue_*.tmpl`) и контекст `services.yaml`
  (`codex.extraTools`, `codex.projectContext`, `codex.servicesOverview`, `codex.links`).

Примечания:

- `prompt run` получает контекст из `CODEXCTL_ISSUE_NUMBER`/`CODEXCTL_PR_NUMBER`, режим из `CODEXCTL_RESUME`,
  флаг деградации из `CODEXCTL_INFRA_UNHEALTHY`, дополнительные переменные из `CODEXCTL_VARS`/`CODEXCTL_VAR_FILE`
  (флаги остаются поддержанными, но в CI рекомендуются `CODEXCTL_*`).
- `CODEXCTL_LANG` задаёт язык промптов и сообщений инструментов.
- Дополнительно можно задать модель и степень рассуждений: `--model` и `--reasoning-effort`.
- Переменные окружения: `CODEXCTL_MODEL`, `CODEXCTL_MODEL_REASONING_EFFORT` (ниже по приоритету, чем флаги и лейблы).
- Допустимые значения модели: `gpt-5.2-codex`, `gpt-5.2`, `gpt-5.1-codex-max`, `gpt-5.1-codex-mini`.
- Допустимые значения степени рассуждений: `low`, `medium`, `high`, `extra-high`.
- `--template` переопределяет `--kind`; если `--kind` не задан, по умолчанию используется `dev_issue`.

### 🧭 5.8. `plan`

Команды для работы с планами и структурой связанных задач:

- `plan resolve-root` — найти «родительский» планирующий Issue для конкретной задачи:

  ```bash
  CODEXCTL_ISSUE_NUMBER=123 \
  CODEXCTL_REPO=owner/codex-project \
  codexctl plan resolve-root

  ```

  Команда использует:
  - лейбл `[ai-plan]` на корневом планирующем Issue;
  - маркер `AI-PLAN-PARENT: #<root>` в теле дочерних Issue.

Такой механизм позволяет строить древовидную структуру задач: один планирующий Issue с `[ai-plan]` описывает
архитектуру и этапы, а дочерние Issue с `AI-PLAN-PARENT: #<root>` реализуются отдельными AI-dev слотами (`[ai-dev]`)
через `ci ensure-ready` и `prompt run`.

### 🔄 5.9. `pr review-apply`

- Автоматически применяет изменения, сделанные Codex‑агентом в AI-dev окружении, к PR:

  ```bash
  CODEXCTL_ENV=ai \
  CODEXCTL_SLOT=1 \
  CODEXCTL_PR_NUMBER=42 \
  CODEXCTL_CODE_ROOT_BASE="/srv/codex/envs" \
  CODEXCTL_LANG=ru \
  codexctl pr review-apply
  ```

- Команда:
  - делает `git add/commit/push` в ветку PR;
  - оставляет комментарий в PR со ссылками на окружение.

`pr detect` — находит PR по ветке и пишет `codexctl_pr_number` в `GITHUB_OUTPUT`.

```bash
export CODEXCTL_BRANCH="codex/issue-123"
export CODEXCTL_REPO="owner/repo"
codexctl pr detect
```

---

## 🌍 6. Переменные окружения

`codexctl` использует объединённую карту переменных:

- переменные процесса (`os.Environ()`);
- переменные из `envFiles` в `services.yaml`;
- переменные из `--var-file` и `--vars`.

Через функцию `envOr` эти переменные доступны в шаблонах:

```yaml
registry: '{{ envOr "CODEXCTL_REGISTRY_HOST" (printf "registry.%s-ai-staging.svc.cluster.local:5000" .Project) }}'
```

Часто используемые переменные:

- `CODEXCTL_KUBECONFIG` — путь до kubeconfig, если не задан в `environments.*.kubeconfig`;
- `CODEXCTL_REGISTRY_HOST` — адрес реестра образов;
- `CODEXCTL_WORKSPACE_MOUNT` — точка монтирования PVC с исходниками (обычно `/workspace`);
- `CODEXCTL_CODE_ROOT_BASE` — базовый путь внутри workspace PVC, используется для вычисления путей:
  - `slotCodeRoot` (например, `.../<slot>/src/...`) и
  - `aiStagingCodeRoot` (например, `.../ai-staging/src/...`),
  которые затем применяются в `services.*.overlays.*.pvcMounts` (см. заголовок‑комментарии в `services.yaml`).
- `CODEXCTL_WORKSPACE_PVC`, `CODEXCTL_DATA_PVC`, `CODEXCTL_REGISTRY_PVC` — имена PVC;
- `CODEXCTL_STORAGE_CLASS_WORKSPACE`, `CODEXCTL_STORAGE_CLASS_DATA`, `CODEXCTL_STORAGE_CLASS_REGISTRY` — StorageClass для PVC;
- `CODEXCTL_BASE_DOMAIN_DEV`, `CODEXCTL_BASE_DOMAIN_AI_STAGING`, `CODEXCTL_BASE_DOMAIN_AI` — домены;
- `CODEXCTL_SYNC_IMAGE` — образ для sync‑пода при копировании исходников;
- `CODEXCTL_KANIKO_EXECUTOR` — путь к kaniko executor (по умолчанию `/kaniko/executor`);
- `CODEXCTL_KANIKO_INSECURE`, `CODEXCTL_KANIKO_SKIP_TLS_VERIFY`, `CODEXCTL_KANIKO_SKIP_TLS_VERIFY_PULL` — флаги для работы с insecure/TLS‑невалидным registry.

В GitHub Actions обычно задаются:

- `GITHUB_RUN_ID`, `CODEXCTL_REPO`, `CODEXCTL_DEV_SLOTS_MAX` — для связи слотов и CI‑запусков;
- секреты подключения к БД/Redis/кешам и другим внешним сервисам;
- `CODEXCTL_GH_PAT`, `CODEXCTL_GH_USERNAME` — токен и имя пользователя для GitHub‑бота.
- `CONTEXT7_API_KEY` — API‑ключ для Context7 (если используется).
- `OPENAI_API_KEY` — API‑ключ OpenAI.

---

## 🔐 7. Интеграция с GitHub Actions и секреты

Ниже — примеры workflow’ов, которые используются в проекте‑примере (смотри также
в репозитории project-example: `.github/workflows/*.yml`). Предполагается self-hosted runner, где уже установлены:
`codexctl`, `kubectl`, `gh`, `kaniko`.

### 🚀 7.1. Deploy ai-staging (push в `main`)

```yaml
name: "AI Staging deploy 🚀"

on:
  push:
    branches: [main]

env:
  CODEXCTL_GH_USERNAME:    ${{ vars.CODEXCTL_GH_USERNAME }}
  CODEXCTL_GH_EMAIL:       ${{ vars.CODEXCTL_GH_EMAIL }}
  CODEXCTL_CODE_ROOT_BASE: ${{ vars.CODEXCTL_CODE_ROOT_BASE }}
  CODEXCTL_BASE_DOMAIN_DEV:        ${{ vars.CODEXCTL_BASE_DOMAIN_DEV }}
  CODEXCTL_BASE_DOMAIN_AI_STAGING: ${{ vars.CODEXCTL_BASE_DOMAIN_AI_STAGING }}
  CODEXCTL_BASE_DOMAIN_AI:         ${{ vars.CODEXCTL_BASE_DOMAIN_AI }}
  CODEXCTL_STORAGE_CLASS_WORKSPACE: ${{ vars.CODEXCTL_STORAGE_CLASS_WORKSPACE }}
  CODEXCTL_STORAGE_CLASS_DATA:      ${{ vars.CODEXCTL_STORAGE_CLASS_DATA }}
  CODEXCTL_STORAGE_CLASS_REGISTRY:  ${{ vars.CODEXCTL_STORAGE_CLASS_REGISTRY }}
  CODEXCTL_KUBECONFIG:    ${{ vars.CODEXCTL_KUBECONFIG }}
  CODEXCTL_WORKSPACE_MOUNT: /workspace
  CODEXCTL_WORKSPACE_PVC:   ${{ vars.CODEXCTL_WORKSPACE_PVC }}
  CODEXCTL_DATA_PVC:        ${{ vars.CODEXCTL_DATA_PVC }}
  CODEXCTL_REGISTRY_PVC:    ${{ vars.CODEXCTL_REGISTRY_PVC }}
  CODEXCTL_REGISTRY_HOST:   ${{ vars.CODEXCTL_REGISTRY_HOST }}
  CODEXCTL_SYNC_IMAGE:      ${{ vars.CODEXCTL_SYNC_IMAGE }}
  CODEXCTL_ENV:            ai-staging
  CODEXCTL_WORKSPACE_UID:  ${{ vars.CODEXCTL_WORKSPACE_UID }}
  CODEXCTL_WORKSPACE_GID:  ${{ vars.CODEXCTL_WORKSPACE_GID }}
  CODEXCTL_REPO:           ${{ github.repository }}

concurrency:
  group: ai-staging-deploy
  cancel-in-progress: false

jobs:
  deploy:
    name: "Deploy ai-staging via codexctl 🚀"
    if: >
      !contains(github.event.head_commit.message, '[skip ci]') &&
      !contains(github.event.head_commit.message, '[skip-ci]') &&
      !contains(github.event.head_commit.message, '[no ci]') &&
      !contains(github.event.head_commit.message, '[no-ci]')
    runs-on: self-hosted
    environment: ai-staging
    steps:
      - name: "Checkout project-example 📥"
        uses: actions/checkout@v4
        with:
          ref: ${{ github.sha }}
          token: ${{ secrets.CODEXCTL_GH_PAT }}

      - name: "Sync ai-staging sources 📂"
        run: |
          set -euo pipefail
          codexctl ci sync-sources

      - name: "Prepare images via codexctl 🪞🏗️"
        env:
          CODEXCTL_MIRROR_IMAGES: true
          CODEXCTL_BUILD_IMAGES:  true
          CODEXCTL_KANIKO_INSECURE:        true
          CODEXCTL_KANIKO_SKIP_TLS_VERIFY: true
          CODEXCTL_KANIKO_SKIP_TLS_VERIFY_PULL: true
        run: |
          set -euo pipefail
          codexctl ci images

      - name: "Apply ai-staging via codexctl 🚀"
        env:
          NO_PROXY:             127.0.0.1,localhost,::1
          GITHUB_RUN_ID:        ${{ github.run_id }}
          CODEXCTL_GH_PAT:         ${{ secrets.CODEXCTL_GH_PAT }}
          CODEXCTL_PREFLIGHT:      true
          CODEXCTL_WAIT:           true
          OPENAI_API_KEY:       ${{ secrets.OPENAI_API_KEY }}
          CONTEXT7_API_KEY:     ${{ secrets.CONTEXT7_API_KEY }}
        run: |
          set -euo pipefail
          codexctl ci apply
```

### 🧭 7.2. AI Plan (планирование по Issue: лейбл `[ai-plan]`)

Ключевые идеи:

- workflow триггерится только для `[ai-plan]` и только для акторов из `CODEXCTL_ALLOWED_USERS`;
- создаёт/находит слот по Issue и поднимает AI-dev окружение через `ci ensure-ready`;
- запускает агента планирования `prompt run --kind plan_issue`;
- на сбое чистит слот через `manage-env cleanup`.

```yaml
name: "AI Plan 🧭"

on:
  issues:
    types: [labeled]

env:
  CODEXCTL_ALLOWED_USERS:  ${{ vars.CODEXCTL_ALLOWED_USERS }}
  CODEXCTL_GH_USERNAME:    ${{ vars.CODEXCTL_GH_USERNAME }}
  CODEXCTL_GH_EMAIL:       ${{ vars.CODEXCTL_GH_EMAIL }}
  CODEXCTL_ENV:            ai
  CODEXCTL_LANG:           ${{ vars.CODEXCTL_LANG }}
  CODEXCTL_DEV_SLOTS_MAX:  ${{ vars.CODEXCTL_DEV_SLOTS_MAX }}
  CODEXCTL_CODE_ROOT_BASE: ${{ vars.CODEXCTL_CODE_ROOT_BASE }}
  CODEXCTL_BASE_DOMAIN_DEV:        ${{ vars.CODEXCTL_BASE_DOMAIN_DEV }}
  CODEXCTL_BASE_DOMAIN_AI_STAGING: ${{ vars.CODEXCTL_BASE_DOMAIN_AI_STAGING }}
  CODEXCTL_BASE_DOMAIN_AI:         ${{ vars.CODEXCTL_BASE_DOMAIN_AI }}
  CODEXCTL_STORAGE_CLASS_WORKSPACE: ${{ vars.CODEXCTL_STORAGE_CLASS_WORKSPACE }}
  CODEXCTL_STORAGE_CLASS_DATA:      ${{ vars.CODEXCTL_STORAGE_CLASS_DATA }}
  CODEXCTL_STORAGE_CLASS_REGISTRY:  ${{ vars.CODEXCTL_STORAGE_CLASS_REGISTRY }}
  CODEXCTL_KUBECONFIG:    ${{ vars.CODEXCTL_KUBECONFIG }}
  CODEXCTL_WORKSPACE_MOUNT: /workspace
  CODEXCTL_WORKSPACE_PVC:   ${{ vars.CODEXCTL_WORKSPACE_PVC }}
  CODEXCTL_DATA_PVC:        ${{ vars.CODEXCTL_DATA_PVC }}
  CODEXCTL_REGISTRY_PVC:    ${{ vars.CODEXCTL_REGISTRY_PVC }}
  CODEXCTL_REGISTRY_HOST:   ${{ vars.CODEXCTL_REGISTRY_HOST }}
  CODEXCTL_SYNC_IMAGE:      ${{ vars.CODEXCTL_SYNC_IMAGE }}
  CODEXCTL_WORKSPACE_UID:  ${{ vars.CODEXCTL_WORKSPACE_UID }}
  CODEXCTL_WORKSPACE_GID:  ${{ vars.CODEXCTL_WORKSPACE_GID }}
  CODEXCTL_REPO:           ${{ github.repository }}

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
    environment: ai-staging
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
          CODEXCTL_ISSUE_NUMBER:   ${{ github.event.issue.number }}
        run: |
          set -euo pipefail
          codexctl ci ensure-slot

  deploy-ai-plan:
    needs: [create-ai-plan]
    name: "Deploy AI plan env 🚀"
    runs-on: self-hosted
    environment: ai-staging
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
          CODEXCTL_SLOT:           ${{ needs.create-ai-plan.outputs.slot }}
          CODEXCTL_ISSUE_NUMBER:   ${{ github.event.issue.number }}
          CODEXCTL_SOURCE:         .
          CODEXCTL_PREPARE_IMAGES: true
          CODEXCTL_APPLY:          true
          CODEXCTL_FORCE_APPLY:    true
          CODEXCTL_WAIT_SOFT_FAIL: true
          OPENAI_API_KEY:          ${{ secrets.OPENAI_API_KEY }}
          CONTEXT7_API_KEY:        ${{ secrets.CONTEXT7_API_KEY }}
        run: |
          set -euo pipefail
          codexctl ci ensure-ready

  run-codex-plan:
    needs: [create-ai-plan, deploy-ai-plan]
    name: "Run planning agent 🤖"
    runs-on: self-hosted
    environment: ai-staging
    env:
      CODEXCTL_GH_PAT:         ${{ secrets.CODEXCTL_GH_PAT }}
      CODEXCTL_INFRA_UNHEALTHY: ${{ needs.deploy-ai-plan.outputs.infra_unhealthy }}
    steps:
      - name: "Checkout default branch 📥"
        uses: actions/checkout@v4
        with:
          token: ${{ secrets.CODEXCTL_GH_PAT }}

      - name: "Run planning agent inline 🤖"
        env:
          GITHUB_RUN_ID:           ${{ github.run_id }}
          CODEXCTL_GH_PAT:         ${{ secrets.CODEXCTL_GH_PAT }}
          CODEXCTL_SLOT:           ${{ needs.create-ai-plan.outputs.slot }}
          CODEXCTL_ISSUE_NUMBER:   ${{ github.event.issue.number }}
          CODEXCTL_NAMESPACE:      ${{ needs.create-ai-plan.outputs.namespace }}
          OPENAI_API_KEY:          ${{ secrets.OPENAI_API_KEY }}
          CONTEXT7_API_KEY:        ${{ secrets.CONTEXT7_API_KEY }}
        run: |
          set -euo pipefail
          codexctl prompt run --kind plan_issue

  cleanup-ai-plan:
    needs: [create-ai-plan, deploy-ai-plan, run-codex-plan]
    if: >
      always() &&
      (needs.create-ai-plan.result != 'success' || needs.deploy-ai-plan.result != 'success' || needs.run-codex-plan.result != 'success')
    name: "Cleanup plan env on failure 🧹"
    runs-on: self-hosted
    environment: ai-staging
    env:
      CODEXCTL_GH_PAT:   ${{ secrets.CODEXCTL_GH_PAT }}
    steps:
      - name: "Checkout minimal 📥"
        uses: actions/checkout@v4
        with:
          token: ${{ secrets.CODEXCTL_GH_PAT }}

      - name: "Cleanup AI plan slot on failure (global) 🧹"
        env:
          CODEXCTL_ISSUE_NUMBER: ${{ github.event.issue.number }}
          CODEXCTL_WITH_CONFIGMAP: true
        run: |
          set -euo pipefail
          codexctl manage-env cleanup || true
```

### 👁 7.3. AI Plan Review (review результатов планирования по комментариям)

Триггер: новый comment в Issue (не PR), который содержит `[ai-plan]`. В workflow делается:

1) `codexctl plan resolve-root` — найти корневую планирующую Issue для текущей (подзадачи/эпика).
2) `ci ensure-ready` — поднять окружение (если ещё не поднято), с `CODEXCTL_ISSUE_NUMBER=<ROOT>`.
3) `prompt run --kind plan_review` с `CODEXCTL_FOCUS_ISSUE_NUMBER=<...>` — сфокусировать агента на конкретной задаче/комментарии.

```yaml
name: "AI Plan Review 👁"

on:
  issue_comment:
    types: [created]

env:
  CODEXCTL_ALLOWED_USERS:  ${{ vars.CODEXCTL_ALLOWED_USERS }}
  CODEXCTL_GH_USERNAME:    ${{ vars.CODEXCTL_GH_USERNAME }}
  CODEXCTL_GH_EMAIL:       ${{ vars.CODEXCTL_GH_EMAIL }}
  CODEXCTL_ENV:            ai
  CODEXCTL_LANG:           ${{ vars.CODEXCTL_LANG }}
  CODEXCTL_DEV_SLOTS_MAX:  ${{ vars.CODEXCTL_DEV_SLOTS_MAX }}
  CODEXCTL_CODE_ROOT_BASE: ${{ vars.CODEXCTL_CODE_ROOT_BASE }}
  CODEXCTL_BASE_DOMAIN_DEV:        ${{ vars.CODEXCTL_BASE_DOMAIN_DEV }}
  CODEXCTL_BASE_DOMAIN_AI_STAGING: ${{ vars.CODEXCTL_BASE_DOMAIN_AI_STAGING }}
  CODEXCTL_BASE_DOMAIN_AI:         ${{ vars.CODEXCTL_BASE_DOMAIN_AI }}
  CODEXCTL_STORAGE_CLASS_WORKSPACE: ${{ vars.CODEXCTL_STORAGE_CLASS_WORKSPACE }}
  CODEXCTL_STORAGE_CLASS_DATA:      ${{ vars.CODEXCTL_STORAGE_CLASS_DATA }}
  CODEXCTL_STORAGE_CLASS_REGISTRY:  ${{ vars.CODEXCTL_STORAGE_CLASS_REGISTRY }}
  CODEXCTL_KUBECONFIG:    ${{ vars.CODEXCTL_KUBECONFIG }}
  CODEXCTL_WORKSPACE_MOUNT: /workspace
  CODEXCTL_WORKSPACE_PVC:   ${{ vars.CODEXCTL_WORKSPACE_PVC }}
  CODEXCTL_DATA_PVC:        ${{ vars.CODEXCTL_DATA_PVC }}
  CODEXCTL_REGISTRY_PVC:    ${{ vars.CODEXCTL_REGISTRY_PVC }}
  CODEXCTL_REGISTRY_HOST:   ${{ vars.CODEXCTL_REGISTRY_HOST }}
  CODEXCTL_SYNC_IMAGE:      ${{ vars.CODEXCTL_SYNC_IMAGE }}
  CODEXCTL_WORKSPACE_UID:  ${{ vars.CODEXCTL_WORKSPACE_UID }}
  CODEXCTL_WORKSPACE_GID:  ${{ vars.CODEXCTL_WORKSPACE_GID }}
  CODEXCTL_REPO:           ${{ github.repository }}

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
    environment: ai-staging
    env:
      CODEXCTL_GH_PAT:         ${{ secrets.CODEXCTL_GH_PAT }}
      GITHUB_RUN_ID:        ${{ github.run_id }}
      OPENAI_API_KEY:       ${{ secrets.OPENAI_API_KEY }}
      CONTEXT7_API_KEY:     ${{ secrets.CONTEXT7_API_KEY }}
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
          CODEXCTL_ISSUE_NUMBER:   ${{ steps.root_issue.outputs.root }}
          CODEXCTL_SOURCE:         .
          CODEXCTL_PREPARE_IMAGES: true
          CODEXCTL_APPLY:          true
        run: |
          set -euo pipefail
          codexctl ci ensure-ready

      - name: "Run planning review agent via codexctl 🤖"
        env:
          GITHUB_RUN_ID:           ${{ github.run_id }}
          CODEXCTL_GH_PAT:         ${{ secrets.CODEXCTL_GH_PAT }}
          CODEXCTL_SLOT:           ${{ steps.card.outputs.slot }}
          CODEXCTL_NAMESPACE:      ${{ steps.card.outputs.namespace }}
          CODEXCTL_ISSUE_NUMBER:   ${{ steps.root_issue.outputs.root }}
          CODEXCTL_FOCUS_ISSUE_NUMBER: ${{ steps.root_issue.outputs.focus }}
          CODEXCTL_PROMPT_CONTINUATION: ${{ steps.card.outputs.codexctl_new_env == 'true' && 'true' || 'false' }}
          CODEXCTL_RESUME:         ${{ steps.card.outputs.codexctl_new_env == 'true' && 'false' || 'true' }}
          OPENAI_API_KEY:          ${{ secrets.OPENAI_API_KEY }}
          CONTEXT7_API_KEY:        ${{ secrets.CONTEXT7_API_KEY }}
        run: |
          set -euo pipefail
          codexctl prompt run --kind plan_review
```

### 🛠 7.4. AI Dev по Issue (лейбл `[ai-dev]`)

Workflow:

1) Проверить, что лейбл `[ai-dev]` и актор входит в `CODEXCTL_ALLOWED_USERS`.
2) `ci ensure-slot` — выбрать/создать слот (значения берутся из `CODEXCTL_ENV=ai`, `CODEXCTL_ISSUE_NUMBER=<N>`,
   `CODEXCTL_DEV_SLOTS_MAX`).
3) `ci ensure-ready` — поднять AI-dev окружение (`CODEXCTL_ENV=ai`, `CODEXCTL_SLOT=<slot>`, `CODEXCTL_ISSUE_NUMBER=<N>`,
   `CODEXCTL_PREPARE_IMAGES=true`, `CODEXCTL_APPLY=true`).
4) Подготовить рабочую ветку в workspace слота (`codex/issue-<N>`).
5) `prompt run --kind dev_issue` — запустить dev‑агента (если infra нездорова — выставить `CODEXCTL_INFRA_UNHEALTHY=true`).
6) auto-commit → push, найти PR по ветке, прикрепить PR к слоту (`manage-env set`) и
   запостить комментарий со ссылками (`manage-env comment-pr`).
7) На сбое — cleanup (`manage-env cleanup` с `CODEXCTL_ENV`/`CODEXCTL_SLOT`/`CODEXCTL_ISSUE_NUMBER` и `CODEXCTL_WITH_CONFIGMAP=true`).

```yaml
name: "AI Dev Issue 🛠"

on:
  issues:
    types: [labeled]

env:
  CODEXCTL_ALLOWED_USERS:  ${{ vars.CODEXCTL_ALLOWED_USERS }}
  CODEXCTL_GH_USERNAME:    ${{ vars.CODEXCTL_GH_USERNAME }}
  CODEXCTL_GH_EMAIL:       ${{ vars.CODEXCTL_GH_EMAIL }}
  CODEXCTL_ENV:            ai
  CODEXCTL_LANG:           ${{ vars.CODEXCTL_LANG }}
  CODEXCTL_DEV_SLOTS_MAX:  ${{ vars.CODEXCTL_DEV_SLOTS_MAX }}
  CODEXCTL_CODE_ROOT_BASE: ${{ vars.CODEXCTL_CODE_ROOT_BASE }}
  CODEXCTL_BASE_DOMAIN_DEV:        ${{ vars.CODEXCTL_BASE_DOMAIN_DEV }}
  CODEXCTL_BASE_DOMAIN_AI_STAGING: ${{ vars.CODEXCTL_BASE_DOMAIN_AI_STAGING }}
  CODEXCTL_BASE_DOMAIN_AI:         ${{ vars.CODEXCTL_BASE_DOMAIN_AI }}
  CODEXCTL_STORAGE_CLASS_WORKSPACE: ${{ vars.CODEXCTL_STORAGE_CLASS_WORKSPACE }}
  CODEXCTL_STORAGE_CLASS_DATA:      ${{ vars.CODEXCTL_STORAGE_CLASS_DATA }}
  CODEXCTL_STORAGE_CLASS_REGISTRY:  ${{ vars.CODEXCTL_STORAGE_CLASS_REGISTRY }}
  CODEXCTL_KUBECONFIG:    ${{ vars.CODEXCTL_KUBECONFIG }}
  CODEXCTL_WORKSPACE_MOUNT: /workspace
  CODEXCTL_WORKSPACE_PVC:   ${{ vars.CODEXCTL_WORKSPACE_PVC }}
  CODEXCTL_DATA_PVC:        ${{ vars.CODEXCTL_DATA_PVC }}
  CODEXCTL_REGISTRY_PVC:    ${{ vars.CODEXCTL_REGISTRY_PVC }}
  CODEXCTL_REGISTRY_HOST:   ${{ vars.CODEXCTL_REGISTRY_HOST }}
  CODEXCTL_SYNC_IMAGE:      ${{ vars.CODEXCTL_SYNC_IMAGE }}
  CODEXCTL_WORKSPACE_UID:  ${{ vars.CODEXCTL_WORKSPACE_UID }}
  CODEXCTL_WORKSPACE_GID:  ${{ vars.CODEXCTL_WORKSPACE_GID }}
  CODEXCTL_REPO:           ${{ github.repository }}

concurrency:
  group: ai-issue-${{ github.event.issue.number }}
  cancel-in-progress: false

jobs:
  create-ai:
    name: "Allocate slot 🧩"
    if: github.event.label.name == '[ai-dev]' && contains(format(',{0},', vars.CODEXCTL_ALLOWED_USERS), format(',{0},', github.actor))
    runs-on: self-hosted
    timeout-minutes: 360
    environment: ai-staging
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
          CODEXCTL_ISSUE_NUMBER:   ${{ github.event.issue.number }}
        run: |
          set -euo pipefail
          codexctl ci ensure-slot

  deploy-ai:
    needs: [create-ai]
    name: "Deploy AI environment 🚀"
    runs-on: self-hosted
    environment: ai-staging
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
          CODEXCTL_SLOT:           ${{ needs.create-ai.outputs.slot }}
          CODEXCTL_ISSUE_NUMBER:   ${{ github.event.issue.number }}
          CODEXCTL_SOURCE:         .
          CODEXCTL_PREPARE_IMAGES: true
          CODEXCTL_APPLY:          true
          CODEXCTL_FORCE_APPLY:    true
          CODEXCTL_WAIT_SOFT_FAIL: true
          OPENAI_API_KEY:          ${{ secrets.OPENAI_API_KEY }}
          CONTEXT7_API_KEY:        ${{ secrets.CONTEXT7_API_KEY }}
        run: |
          set -euo pipefail
          codexctl ci ensure-ready

  run-codex:
    needs: [create-ai, deploy-ai]
    name: "Run dev agent 🤖"
    runs-on: self-hosted
    environment: ai-staging
    env:
      CODEXCTL_GH_PAT:   ${{ secrets.CODEXCTL_GH_PAT }}
      CODEXCTL_INFRA_UNHEALTHY: ${{ needs.deploy-ai.outputs.infra_unhealthy }}
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
          CODEXCTL_SLOT:           ${{ needs.create-ai.outputs.slot }}
          CODEXCTL_ISSUE_NUMBER:   ${{ github.event.issue.number }}
          CODEXCTL_NAMESPACE:      ${{ needs.create-ai.outputs.namespace }}
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
          CODEXCTL_BRANCH:       codex/issue-${{ github.event.issue.number }}
          CODEXCTL_GH_PAT:       ${{ secrets.CODEXCTL_GH_PAT }}
        run: |
          set -euo pipefail
          codexctl pr detect

      - name: "Attach PR number to slot 🏷️"
        if: steps.detect_pr.outputs.codexctl_pr_number != ''
        env:
          CODEXCTL_SLOT:     ${{ needs.create-ai.outputs.slot }}
          CODEXCTL_PR_NUMBER: ${{ steps.detect_pr.outputs.codexctl_pr_number }}
        run: |
          set -euo pipefail
          codexctl manage-env set

      - name: "Comment to PR with env links 🔗"
        if: steps.detect_pr.outputs.codexctl_pr_number != ''
        env:
          CODEXCTL_SLOT:      ${{ needs.create-ai.outputs.slot }}
          CODEXCTL_PR_NUMBER: ${{ steps.detect_pr.outputs.codexctl_pr_number }}
          CODEXCTL_GH_PAT:    ${{ secrets.CODEXCTL_GH_PAT }}
        run: |
          set -euo pipefail
          codexctl manage-env comment-pr

  cleanup-ai:
    needs: [create-ai, deploy-ai, run-codex]
    if: >
      always() &&
      (needs.create-ai.result != 'success' || needs.deploy-ai.result != 'success' || needs.run-codex.result != 'success')
    name: "Cleanup on failure 🧹"
    runs-on: self-hosted
    environment: ai-staging
    env:
      CODEXCTL_GH_PAT: ${{ secrets.CODEXCTL_GH_PAT }}
    steps:
      - name: "Checkout minimal 📥"
        uses: actions/checkout@v4
        with:
          token: ${{ secrets.CODEXCTL_GH_PAT }}

      - name: "Cleanup AI slot on failure (global) 🧹"
        env:
          CODEXCTL_ISSUE_NUMBER: ${{ github.event.issue.number }}
          CODEXCTL_WITH_CONFIGMAP: true
        run: |
          set -euo pipefail
          codexctl manage-env cleanup || true
```

Полный пример см. в репозитории project-example: `.github/workflows/ai_dev_issue.yml`.

### 👁 7.5. AI PR Review (авто‑исправление по Changes Requested)

Триггер: submitted review со статусом `changes_requested`. Workflow поднимает окружение по PR, запускает агента
`dev_review`, затем применяет изменения и комментирует PR через `codexctl pr review-apply`.

```yaml
name: "AI PR Review 👁"

on:
  pull_request_review:
    types: [submitted]

env:
  CODEXCTL_ALLOWED_USERS:  ${{ vars.CODEXCTL_ALLOWED_USERS }}
  CODEXCTL_GH_USERNAME:    ${{ vars.CODEXCTL_GH_USERNAME }}
  CODEXCTL_GH_EMAIL:       ${{ vars.CODEXCTL_GH_EMAIL }}
  CODEXCTL_ENV:            ai
  CODEXCTL_LANG:           ${{ vars.CODEXCTL_LANG }}
  CODEXCTL_DEV_SLOTS_MAX:  ${{ vars.CODEXCTL_DEV_SLOTS_MAX }}
  CODEXCTL_CODE_ROOT_BASE: ${{ vars.CODEXCTL_CODE_ROOT_BASE }}
  CODEXCTL_BASE_DOMAIN_DEV:        ${{ vars.CODEXCTL_BASE_DOMAIN_DEV }}
  CODEXCTL_BASE_DOMAIN_AI_STAGING: ${{ vars.CODEXCTL_BASE_DOMAIN_AI_STAGING }}
  CODEXCTL_BASE_DOMAIN_AI:         ${{ vars.CODEXCTL_BASE_DOMAIN_AI }}
  CODEXCTL_STORAGE_CLASS_WORKSPACE: ${{ vars.CODEXCTL_STORAGE_CLASS_WORKSPACE }}
  CODEXCTL_STORAGE_CLASS_DATA:      ${{ vars.CODEXCTL_STORAGE_CLASS_DATA }}
  CODEXCTL_STORAGE_CLASS_REGISTRY:  ${{ vars.CODEXCTL_STORAGE_CLASS_REGISTRY }}
  CODEXCTL_KUBECONFIG:    ${{ vars.CODEXCTL_KUBECONFIG }}
  CODEXCTL_WORKSPACE_MOUNT: /workspace
  CODEXCTL_WORKSPACE_PVC:   ${{ vars.CODEXCTL_WORKSPACE_PVC }}
  CODEXCTL_DATA_PVC:        ${{ vars.CODEXCTL_DATA_PVC }}
  CODEXCTL_REGISTRY_PVC:    ${{ vars.CODEXCTL_REGISTRY_PVC }}
  CODEXCTL_REGISTRY_HOST:   ${{ vars.CODEXCTL_REGISTRY_HOST }}
  CODEXCTL_SYNC_IMAGE:      ${{ vars.CODEXCTL_SYNC_IMAGE }}
  CODEXCTL_WORKSPACE_UID:  ${{ vars.CODEXCTL_WORKSPACE_UID }}
  CODEXCTL_WORKSPACE_GID:  ${{ vars.CODEXCTL_WORKSPACE_GID }}
  CODEXCTL_PR_NUMBER:      ${{ github.event.pull_request.number }}
  CODEXCTL_BRANCH:         ${{ github.event.pull_request.head.ref }}
  CODEXCTL_REPO:           ${{ github.repository }}

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
    environment: ai-staging
    env:
      CODEXCTL_GH_PAT:         ${{ secrets.CODEXCTL_GH_PAT }}
      GITHUB_RUN_ID:        ${{ github.run_id }}
      OPENAI_API_KEY:       ${{ secrets.OPENAI_API_KEY }}
      CONTEXT7_API_KEY:     ${{ secrets.CONTEXT7_API_KEY }}
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
          CODEXCTL_SOURCE:        .
          CODEXCTL_PREPARE_IMAGES: true
          CODEXCTL_APPLY:          true
        run: |
          set -euo pipefail
          codexctl ci ensure-ready

      - name: "Run Codex review-fix agent 🤖"
        env:
          GITHUB_RUN_ID:           ${{ github.run_id }}
          CODEXCTL_GH_PAT:         ${{ secrets.CODEXCTL_GH_PAT }}
          CODEXCTL_SLOT:           ${{ steps.card.outputs.slot }}
          CODEXCTL_NAMESPACE:      ${{ steps.card.outputs.namespace }}
          CODEXCTL_PROMPT_CONTINUATION: ${{ steps.card.outputs.codexctl_new_env == 'true' && 'true' || 'false' }}
          CODEXCTL_RESUME:         ${{ steps.card.outputs.codexctl_new_env == 'true' && 'false' || 'true' }}
          OPENAI_API_KEY:          ${{ secrets.OPENAI_API_KEY }}
          CONTEXT7_API_KEY:        ${{ secrets.CONTEXT7_API_KEY }}
        run: |
          set -euo pipefail
          codexctl prompt run --kind dev_review

      - name: "Apply review changes and comment 💾"
        env:
          CODEXCTL_SLOT:        ${{ steps.card.outputs.slot }}
          CODEXCTL_GH_PAT:      ${{ secrets.CODEXCTL_GH_PAT }}
        run: |
          set -euo pipefail
          codexctl pr review-apply
```

Полный пример см. в репозитории project-example: `.github/workflows/ai_pr_review.yml`.

### 🧯 7.6. AI Staging Repair по Issue (лейбл `[ai-repair]`)

Этот режим поднимает `ai-repair` в namespace `ai-staging` (Pod Codex + RBAC только для нужных ресурсов), синхронизирует исходники ai-staging,
запускает агента `ai-repair_issue`, и при необходимости пушит изменения в ветку `codex/ai-repair-<N>`.

```yaml
name: "AI Staging Repair 🧯"

on:
  issues:
    types: [labeled]

env:
  CODEXCTL_ALLOWED_USERS:  ${{ vars.CODEXCTL_ALLOWED_USERS }}
  CODEXCTL_GH_USERNAME:    ${{ vars.CODEXCTL_GH_USERNAME }}
  CODEXCTL_GH_EMAIL:       ${{ vars.CODEXCTL_GH_EMAIL }}
  CODEXCTL_ENV:            ai-repair
  CODEXCTL_LANG:           ${{ vars.CODEXCTL_LANG }}
  CODEXCTL_DEV_SLOTS_MAX:  ${{ vars.CODEXCTL_DEV_SLOTS_MAX }}
  CODEXCTL_CODE_ROOT_BASE: ${{ vars.CODEXCTL_CODE_ROOT_BASE }}
  CODEXCTL_BASE_DOMAIN_DEV:        ${{ vars.CODEXCTL_BASE_DOMAIN_DEV }}
  CODEXCTL_BASE_DOMAIN_AI_STAGING: ${{ vars.CODEXCTL_BASE_DOMAIN_AI_STAGING }}
  CODEXCTL_BASE_DOMAIN_AI:         ${{ vars.CODEXCTL_BASE_DOMAIN_AI }}
  CODEXCTL_STORAGE_CLASS_WORKSPACE: ${{ vars.CODEXCTL_STORAGE_CLASS_WORKSPACE }}
  CODEXCTL_STORAGE_CLASS_DATA:      ${{ vars.CODEXCTL_STORAGE_CLASS_DATA }}
  CODEXCTL_STORAGE_CLASS_REGISTRY:  ${{ vars.CODEXCTL_STORAGE_CLASS_REGISTRY }}
  CODEXCTL_KUBECONFIG:    ${{ vars.CODEXCTL_KUBECONFIG }}
  CODEXCTL_WORKSPACE_MOUNT: /workspace
  CODEXCTL_WORKSPACE_PVC:   ${{ vars.CODEXCTL_WORKSPACE_PVC }}
  CODEXCTL_DATA_PVC:        ${{ vars.CODEXCTL_DATA_PVC }}
  CODEXCTL_REGISTRY_PVC:    ${{ vars.CODEXCTL_REGISTRY_PVC }}
  CODEXCTL_REGISTRY_HOST:   ${{ vars.CODEXCTL_REGISTRY_HOST }}
  CODEXCTL_SYNC_IMAGE:      ${{ vars.CODEXCTL_SYNC_IMAGE }}
  CODEXCTL_WORKSPACE_UID:  ${{ vars.CODEXCTL_WORKSPACE_UID }}
  CODEXCTL_WORKSPACE_GID:  ${{ vars.CODEXCTL_WORKSPACE_GID }}
  CODEXCTL_REPO:           ${{ github.repository }}

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
    environment: ai-staging
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
          CODEXCTL_ISSUE_NUMBER:   ${{ github.event.issue.number }}
        run: |
          set -euo pipefail
          codexctl ci ensure-slot

  deploy-ai-repair:
    needs: [create-ai-repair]
    name: "Deploy ai-staging repair env 🚀"
    runs-on: self-hosted
    environment: ai-staging
    steps:
      - name: "Checkout project-example 📥"
        uses: actions/checkout@v4
        with:
          ref: ${{ github.sha }}
          token: ${{ secrets.CODEXCTL_GH_PAT }}

      - name: "Sync ai-staging sources 📂"
        run: |
          set -euo pipefail
          codexctl ci sync-sources

      - name: "Ensure ai-staging repair env via codexctl 🚀"
        env:
          GITHUB_RUN_ID:           ${{ github.run_id }}
          CODEXCTL_GH_PAT:         ${{ secrets.CODEXCTL_GH_PAT }}
          CODEXCTL_SLOT:           ${{ needs.create-ai-repair.outputs.slot }}
          CODEXCTL_PREFLIGHT:      true
          CODEXCTL_WAIT:           true
          CODEXCTL_ONLY_INFRA:     codex-ai-repair-rbac
          CODEXCTL_ONLY_SERVICES:  codex
          OPENAI_API_KEY:          ${{ secrets.OPENAI_API_KEY }}
          CONTEXT7_API_KEY:        ${{ secrets.CONTEXT7_API_KEY }}
        run: |
          set -euo pipefail
          codexctl ci apply

      - name: "Cleanup ai-staging repair env on failure 🧹"
        if: failure() || cancelled()
        env:
          CODEXCTL_SLOT: ${{ needs.create-ai-repair.outputs.slot }}
          CODEXCTL_WITH_CONFIGMAP: true
        run: |
          set -euo pipefail
          codexctl manage-env cleanup || true

  run-codex:
    needs: [create-ai-repair, deploy-ai-repair]
    name: "Run ai-staging repair agent 🤖"
    runs-on: self-hosted
    environment: ai-staging
    env:
      CODEXCTL_GH_PAT:   ${{ secrets.CODEXCTL_GH_PAT }}
    steps:
      - name: "Checkout default branch 📥"
        uses: actions/checkout@v4
        with:
          token: ${{ secrets.CODEXCTL_GH_PAT }}

      - name: "Sync ai-staging sources 📂"
        run: |
          set -euo pipefail
          codexctl ci sync-sources

      - name: "Ensure working branch 🌿"
        env:
          CODEXCTL_ISSUE_NUMBER: ${{ github.event.issue.number }}
        run: |
          set -euo pipefail
          WORKDIR="${CODEXCTL_CODE_ROOT_BASE}/ai-staging/src"
          cd "${WORKDIR}"
          git config user.name "${CODEXCTL_GH_USERNAME}"
          git config user.email "${CODEXCTL_GH_EMAIL}"
          git checkout -b "codex/ai-repair-${CODEXCTL_ISSUE_NUMBER}" || git checkout "codex/ai-repair-${CODEXCTL_ISSUE_NUMBER}"
        shell: bash

      - name: "Run Codex ai-staging repair agent 🤖"
        env:
          GITHUB_RUN_ID:           ${{ github.run_id }}
          CODEXCTL_GH_PAT:         ${{ secrets.CODEXCTL_GH_PAT }}
          CODEXCTL_SLOT:           ${{ needs.create-ai-repair.outputs.slot }}
          CODEXCTL_NAMESPACE:      ${{ needs.create-ai-repair.outputs.namespace }}
          CODEXCTL_ISSUE_NUMBER:   ${{ github.event.issue.number }}
          OPENAI_API_KEY:          ${{ secrets.OPENAI_API_KEY }}
          CONTEXT7_API_KEY:        ${{ secrets.CONTEXT7_API_KEY }}
        run: |
          set -euo pipefail
          codexctl prompt run --kind ai-repair_issue

      - name: "Cleanup ai-staging repair env on failure 🧹"
        if: failure() || cancelled()
        env:
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
          WORKDIR="${CODEXCTL_CODE_ROOT_BASE}/ai-staging/src"
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

          MSG="fix: ai-staging repair for issue #${CODEXCTL_ISSUE_NUMBER}"
          git commit -m "$MSG"
          git push origin "$BRANCH"

      - name: "Detect PR for issue branch 🔎"
        id: detect_pr
        env:
          CODEXCTL_BRANCH: codex/ai-repair-${{ github.event.issue.number }}
          CODEXCTL_GH_PAT: ${{ secrets.CODEXCTL_GH_PAT }}
        run: |
          set -euo pipefail
          codexctl pr detect

      - name: "Attach PR number to slot 🏷️"
        if: steps.detect_pr.outputs.codexctl_pr_number != ''
        env:
          CODEXCTL_SLOT:     ${{ needs.create-ai-repair.outputs.slot }}
          CODEXCTL_PR_NUMBER: ${{ steps.detect_pr.outputs.codexctl_pr_number }}
        run: |
          set -euo pipefail
          codexctl manage-env set

      - name: "Comment to PR with env links 🔗"
        if: steps.detect_pr.outputs.codexctl_pr_number != ''
        env:
          CODEXCTL_SLOT:      ${{ needs.create-ai-repair.outputs.slot }}
          CODEXCTL_PR_NUMBER: ${{ steps.detect_pr.outputs.codexctl_pr_number }}
          CODEXCTL_GH_PAT:    ${{ secrets.CODEXCTL_GH_PAT }}
        run: |
          set -euo pipefail
          codexctl manage-env comment-pr || true
```

Полный пример см. в репозитории project-example: `.github/workflows/ai_repair_issue.yml`.

### 👁 7.7. AI Staging Repair PR Review (Changes Requested для `codex/ai-repair-*`)

Триггер: `changes_requested` в review и ветка PR начинается с `codex/ai-repair-`. Workflow обеспечивает `ai-repair`
окружение и запускает `ai-repair_review`, затем применяет фиксы через `codexctl pr review-apply`.

```yaml
name: "AI Staging Repair PR Review 👁"

on:
  pull_request_review:
    types: [submitted]

env:
  CODEXCTL_ALLOWED_USERS:  ${{ vars.CODEXCTL_ALLOWED_USERS }}
  CODEXCTL_GH_USERNAME:    ${{ vars.CODEXCTL_GH_USERNAME }}
  CODEXCTL_GH_EMAIL:       ${{ vars.CODEXCTL_GH_EMAIL }}
  CODEXCTL_ENV:            ai-repair
  CODEXCTL_LANG:           ${{ vars.CODEXCTL_LANG }}
  CODEXCTL_DEV_SLOTS_MAX:  ${{ vars.CODEXCTL_DEV_SLOTS_MAX }}
  CODEXCTL_CODE_ROOT_BASE: ${{ vars.CODEXCTL_CODE_ROOT_BASE }}
  CODEXCTL_BASE_DOMAIN_DEV:        ${{ vars.CODEXCTL_BASE_DOMAIN_DEV }}
  CODEXCTL_BASE_DOMAIN_AI_STAGING: ${{ vars.CODEXCTL_BASE_DOMAIN_AI_STAGING }}
  CODEXCTL_BASE_DOMAIN_AI:         ${{ vars.CODEXCTL_BASE_DOMAIN_AI }}
  CODEXCTL_STORAGE_CLASS_WORKSPACE: ${{ vars.CODEXCTL_STORAGE_CLASS_WORKSPACE }}
  CODEXCTL_STORAGE_CLASS_DATA:      ${{ vars.CODEXCTL_STORAGE_CLASS_DATA }}
  CODEXCTL_STORAGE_CLASS_REGISTRY:  ${{ vars.CODEXCTL_STORAGE_CLASS_REGISTRY }}
  CODEXCTL_KUBECONFIG:    ${{ vars.CODEXCTL_KUBECONFIG }}
  CODEXCTL_WORKSPACE_MOUNT: /workspace
  CODEXCTL_WORKSPACE_PVC:   ${{ vars.CODEXCTL_WORKSPACE_PVC }}
  CODEXCTL_DATA_PVC:        ${{ vars.CODEXCTL_DATA_PVC }}
  CODEXCTL_REGISTRY_PVC:    ${{ vars.CODEXCTL_REGISTRY_PVC }}
  CODEXCTL_REGISTRY_HOST:   ${{ vars.CODEXCTL_REGISTRY_HOST }}
  CODEXCTL_SYNC_IMAGE:      ${{ vars.CODEXCTL_SYNC_IMAGE }}
  CODEXCTL_WORKSPACE_UID:  ${{ vars.CODEXCTL_WORKSPACE_UID }}
  CODEXCTL_WORKSPACE_GID:  ${{ vars.CODEXCTL_WORKSPACE_GID }}
  CODEXCTL_PR_NUMBER:      ${{ github.event.pull_request.number }}
  CODEXCTL_BRANCH:         ${{ github.event.pull_request.head.ref }}
  CODEXCTL_REPO:           ${{ github.repository }}

concurrency:
  group: ai-repair-pr-${{ github.event.pull_request.number }}
  cancel-in-progress: false

jobs:
  run:
    name: "AI Staging repair review run 🤖"
    if: >-
      github.event.review.state == 'changes_requested' &&
      startsWith(github.event.pull_request.head.ref, 'codex/ai-repair-') &&
      contains(format(',{0},', vars.CODEXCTL_ALLOWED_USERS), format(',{0},', github.actor))
    runs-on: self-hosted
    environment: ai-staging
    env:
      CODEXCTL_GH_PAT:         ${{ secrets.CODEXCTL_GH_PAT }}
      GITHUB_RUN_ID:        ${{ github.run_id }}
      OPENAI_API_KEY:       ${{ secrets.OPENAI_API_KEY }}
      CONTEXT7_API_KEY:     ${{ secrets.CONTEXT7_API_KEY }}
    steps:
      - name: "Checkout PR head 📥"
        uses: actions/checkout@v4
        with:
          ref:   ${{ github.event.pull_request.head.ref }}
          token: ${{ secrets.CODEXCTL_GH_PAT }}
          fetch-depth: 0

      - name: "Sync ai-staging sources 📂"
        run: |
          set -euo pipefail
          codexctl ci sync-sources

      - name: "Resolve slot and namespace for PR 📇"
        id: card
        run: |
          set -euo pipefail
          codexctl ci ensure-ready

      - name: "Ensure ai-staging repair env via codexctl 🚀"
        env:
          GITHUB_RUN_ID:           ${{ github.run_id }}
          CODEXCTL_GH_PAT:         ${{ secrets.CODEXCTL_GH_PAT }}
          CODEXCTL_SLOT:           ${{ steps.card.outputs.slot }}
          CODEXCTL_PREFLIGHT:      true
          CODEXCTL_WAIT:           true
          CODEXCTL_ONLY_INFRA:     codex-ai-repair-rbac
          CODEXCTL_ONLY_SERVICES:  codex
          OPENAI_API_KEY:          ${{ secrets.OPENAI_API_KEY }}
          CONTEXT7_API_KEY:        ${{ secrets.CONTEXT7_API_KEY }}
        run: |
          set -euo pipefail
          codexctl ci apply

      - name: "Run Codex ai-staging repair review 🤖"
        env:
          GITHUB_RUN_ID:           ${{ github.run_id }}
          CODEXCTL_GH_PAT:         ${{ secrets.CODEXCTL_GH_PAT }}
          CODEXCTL_SLOT:           ${{ steps.card.outputs.slot }}
          CODEXCTL_NAMESPACE:      ${{ steps.card.outputs.namespace }}
          CODEXCTL_PROMPT_CONTINUATION: ${{ (steps.card.outputs.codexctl_new_env == 'true' || steps.card.outputs.codexctl_env_ready != 'true') && 'true' || 'false' }}
          CODEXCTL_RESUME:         ${{ (steps.card.outputs.codexctl_new_env == 'true' || steps.card.outputs.codexctl_env_ready != 'true') && 'false' || 'true' }}
          OPENAI_API_KEY:          ${{ secrets.OPENAI_API_KEY }}
          CONTEXT7_API_KEY:        ${{ secrets.CONTEXT7_API_KEY }}
        run: |
          set -euo pipefail
          codexctl prompt run --kind ai-repair_review

      - name: "Apply review changes and comment 💾"
        env:
          CODEXCTL_SLOT:      ${{ steps.card.outputs.slot }}
          CODEXCTL_GH_PAT:    ${{ secrets.CODEXCTL_GH_PAT }}
        run: |
          set -euo pipefail
          codexctl pr review-apply

      - name: "Cleanup ai-staging repair env on failure 🧹"
        if: (failure() || cancelled()) && steps.card.outputs.slot != ''
        env:
          CODEXCTL_SLOT: ${{ steps.card.outputs.slot }}
          CODEXCTL_WITH_CONFIGMAP: true
        run: |
          set -euo pipefail
          codexctl manage-env cleanup || true
```

Полный пример см. в репозитории project-example: `.github/workflows/ai_repair_pr_review.yml`.

### 🧹 7.8. Cleanup (закрытие Issue/PR)

При закрытии Issue/PR workflow очищает окружения и удаляет ветки `codex/issue-*` / `codex/ai-repair-*`.
Если PR был merged, workflow дополнительно закрывает связанную Issue (по номеру, вытащенному из имени ветки).

```yaml
name: "AI Cleanup 🧹"

on:
  pull_request:
    types: [closed]
  issues:
    types: [closed]

env:
  CODEXCTL_BASE_DOMAIN_DEV:        ${{ vars.CODEXCTL_BASE_DOMAIN_DEV }}
  CODEXCTL_BASE_DOMAIN_AI_STAGING: ${{ vars.CODEXCTL_BASE_DOMAIN_AI_STAGING }}
  CODEXCTL_BASE_DOMAIN_AI:         ${{ vars.CODEXCTL_BASE_DOMAIN_AI }}
  CODEXCTL_STORAGE_CLASS_WORKSPACE: ${{ vars.CODEXCTL_STORAGE_CLASS_WORKSPACE }}
  CODEXCTL_STORAGE_CLASS_DATA:      ${{ vars.CODEXCTL_STORAGE_CLASS_DATA }}
  CODEXCTL_STORAGE_CLASS_REGISTRY:  ${{ vars.CODEXCTL_STORAGE_CLASS_REGISTRY }}
  CODEXCTL_KUBECONFIG:    ${{ vars.CODEXCTL_KUBECONFIG }}
  CODEXCTL_WORKSPACE_MOUNT: /workspace
  CODEXCTL_WORKSPACE_PVC:   ${{ vars.CODEXCTL_WORKSPACE_PVC }}
  CODEXCTL_DATA_PVC:        ${{ vars.CODEXCTL_DATA_PVC }}
  CODEXCTL_REGISTRY_PVC:    ${{ vars.CODEXCTL_REGISTRY_PVC }}
  CODEXCTL_REGISTRY_HOST:   ${{ vars.CODEXCTL_REGISTRY_HOST }}
  CODEXCTL_SYNC_IMAGE:      ${{ vars.CODEXCTL_SYNC_IMAGE }}
  CODEXCTL_WORKSPACE_UID: ${{ vars.CODEXCTL_WORKSPACE_UID }}
  CODEXCTL_WORKSPACE_GID: ${{ vars.CODEXCTL_WORKSPACE_GID }}
  CODEXCTL_PR_NUMBER:     ${{ github.event.pull_request.number || '' }}
  CODEXCTL_BRANCH:        ${{ github.event.pull_request.head.ref || '' }}
  CODEXCTL_REPO:          ${{ github.repository }}

concurrency:
  group: ai-cleanup-${{ github.event_name }}-${{ github.event.pull_request.number || github.event.issue.number }}
  cancel-in-progress: false

jobs:
  cleanup:
    name: "Cleanup AI environments 🧼"
    runs-on: self-hosted
    environment: ai-staging
    env:
      CODEXCTL_GH_PAT: ${{ secrets.CODEXCTL_GH_PAT }}
    steps:
      - name: "Checkout project-example 📥"
        uses: actions/checkout@v4
        with:
          token: ${{ secrets.CODEXCTL_GH_PAT }}

      - name: "Cleanup for PR closed 🧹"
        if: github.event_name == 'pull_request'
        env:
          CODEXCTL_WITH_CONFIGMAP: true
          CODEXCTL_DELETE_BRANCH: true
        run: |
          set -euo pipefail
          codexctl manage-env cleanup-pr

      - name: "Close linked Issue after merge ✅"
        if: github.event_name == 'pull_request' && github.event.pull_request.merged == true
        env:
          CODEXCTL_GH_PAT: ${{ secrets.CODEXCTL_GH_PAT }}
          CODEXCTL_CLOSE_ISSUE: true
        run: |
          set -euo pipefail
          codexctl manage-env close-linked-issue

      - name: "Cleanup for Issue closed 🧹"
        if: github.event_name == 'issues'
        env:
          CODEXCTL_ISSUE_NUMBER: ${{ github.event.issue.number }}
          CODEXCTL_WITH_CONFIGMAP: true
          CODEXCTL_DELETE_BRANCH: true
        run: |
          set -euo pipefail
          codexctl manage-env cleanup-issue
```

Полный пример см. в репозитории project-example: `.github/workflows/ai_cleanup.yml`.

### 🔑 7.9. Секреты и PAT для GitHub‑бота

Рекомендуемый набор секретов/vars в репозитории вашего проекта (например, `codex-project`):

- `CODEXCTL_GH_PAT` — PAT пользователя‑бота GitHub;
- `CODEXCTL_GH_USERNAME` — имя пользователя‑бота; Не используйте личный аккаунт разработчика, создайте отдельный технический аккаунт.
- `CODEXCTL_GH_EMAIL` — email пользователя‑бота для git‑коммитов (например, `codex-bot@example.com`).
- `CODEXCTL_KUBECONFIG` — путь к kubeconfig для ai-staging;
- секреты БД/Redis/кеша/очереди (username/password, DSN и т.п.);
- `CODEXCTL_REGISTRY_HOST` и (опционально) реквизиты доступа к реестру.
- `OPENAI_API_KEY` — API‑ключ OpenAI.
- `CONTEXT7_API_KEY` — API‑ключ для Context7 (если используется).
- `CODEXCTL_ALLOWED_USERS` (vars) — список разрешённых GitHub‑пользователей, в формате `user1,user2,user3`.
- `CODEXCTL_DEV_SLOTS_MAX` (vars) — максимум слотов, который может выделять `ci ensure-slot/ensure-ready`.

Как создать пользователя и PAT:

1. Создать отдельный технический аккаунт GitHub для бота (например, `codex-bot-42`).
2. В настройках аккаунта выбрать **Developer settings → Personal access tokens → Fine-grained**.
3. Создать токен с правами:
   - доступ к репозиторию проекта (например, `codex-project`, read/write для `code`, `pull requests`, `issues`);
   - доступ к Actions (если нужно управлять workflow).
4. Сохранить токен, добавить его в secrets репозитория как `CODEXCTL_GH_PAT`.

---

## 🐳 8. Образ Codex‑агента (пример проекта)

Пример Dockerfile для образа агента находится в репозитории проекта‑примера:
`github.com/codex-k8s/project-example/deploy/codex/Dockerfile`.

В нём есть всё, что нужно агенту внутри pod’а:

- Node + Codex CLI (`@openai/codex`);
- Go toolchain + плагины (`protoc-gen-go`, `protoc-gen-go-grpc`, `wire`);
- `protoc` и стандартные include’ы;
- Python + виртуальное окружение с базовыми библиотеками (`requests`, `httpx`, `redis`, `psycopg[binary]`, `PyYAML`, `ujson`);
- `kubectl`, `gh`, `jq`, `ripgrep`;
- сборка `codexctl` и установка бинаря в `/usr/local/bin`.

Почему это важно: Codex‑агент работает внутри Kubernetes pod’а и не имеет доступа
к инструментам хоста. Отсутствие бинарников (kubectl/gh/protoc и т.д.)
ломает preflight‑проверки и блокирует apply/build/test сценарии.

Такой образ можно указать в `images.codex` и использовать в `services.codex` внутри `services.yaml` вашего проекта
(в примерах — `codex-project`):

- Pod `codex` в каждом AI-dev слоте будет работать именно с этим образом;
- внутри Pod’а доступны `codex`, `codexctl`, `kubectl`, `gh` и другие инструменты.

---

## 🛡️ 9. Безопасность и стабильность

- **Ранняя стадия разработки.** `codexctl` находится на начальном этапе развития, покрытие тестами отсутствует, возможны
  нестабильное поведение и ломающие изменения. Используйте инструмент осмотрительно и закладывайте время на отладку.
- **Только изолированные кластеры.** Предполагается, что `codexctl` и Codex‑агенты работают в **отдельном от продакшена**
  Kubernetes‑кластере, предназначенном для разработки и AI‑экспериментов (dev/ai-staging/ai). **Не используйте** его напрямую
  поверх боевого прод‑кластера.
- **Ограничение внешнего доступа.** Dev/ai-staging/AI-dev окружения должны быть защищены:
  - HTTP‑интерфейсы спрятаны за OAuth2‑proxy/IAP или другим механизмом аутентификации;
  - ingress’ы и сервисы не должны быть напрямую доступны из интернета без авторизации;
  - доступ к kube‑API ограничен по пользователям/рольям.
- **Права Codex‑агента.** Pod `codex` получает расширенные права в namespace слота (создание/обновление деплойментов,
  чтение логов, `exec` в Pod’ы и т.п.). Обязательно:
  - проверяйте RBAC манифесты (Role/RoleBinding) в `deploy/codex` для своего проекта;
  - не выдавайте агенту права на управление критичными namespace’ами;
  - храните kubeconfig и секреты только в защищённых хранилищах (GitHub secrets, Kubernetes secrets, Vault).
- **Используйте с осторожностью.** Автоматические изменения кластера и репозитория, выполняемые Codex‑агентом через
  `codexctl`, должны проходить ревью людей. Планируйте процессы так, чтобы любые изменения, внесённые агентом, проходили
  через PR и ручное утверждение.

Если вы интегрируете `codexctl` в новый проект (`codex-project` или другой), начинайте с небольшого и изолированного
стека, постепенно расширяя сценарии и добавляя проверки (manual review, smoke‑тесты, отдельные namespace’ы/кластера 
для экспериментов).
