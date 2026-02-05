# Архитектура проекта (эталон)

## Контекст

`codexctl` — Go CLI-оркестратор окружений и workflow-сценариев.

- Не микросервисная система.
- Не frontend-приложение.
- Базовые внешние контуры: Kubernetes, GitHub, MCP-экосистема (`yaml-mcp-server`, `telegram-approver`).

## Целевая архитектурная модель

Модель: Ports and Adapters с двумя ключевыми портами платформы:

- `Orchestrator` — управление средой исполнения (сейчас Kubernetes, позже Docker Compose и другие платформы).
- `Repository` — работа с хостингом репозитория и review-процессом (сейчас GitHub, позже GitLab).

Слои и зависимости:

1. `cli` (transport)
   - parsing команд/флагов/env;
   - запуск use-case;
   - без инфраструктурных вызовов.
2. `application` (use-cases)
   - orchestration сценариев `apply/ensure-ready/manage-env/prompt/pr/plan`;
   - зависит только от интерфейсов портов.
3. `core` (policy)
   - инварианты, контракты, правила валидации и вычисления контекста;
   - не знает о конкретных SDK/CLI.
4. `adapters` (infrastructure)
   - реализации `Orchestrator` и `Repository`;
   - state backend, prompt/config rendering и другие внешние интеграции.

Правило зависимостей: `cli -> application -> core`, адаптеры подключаются снаружи.

## Базовые реализации портов

Эталон по умолчанию:

- `Orchestrator`: реализация на `k8s.io/client-go`.
- `Repository`: реализация на `github.com/google/go-github`.

Важно: это именно реализация по умолчанию, а не единственно возможная.
Контракты портов должны позволять добавить:

- `DockerComposeOrchestrator`
- `GitLabRepository`

без изменения прикладных сценариев.

## Границы ответственности

- `codexctl` не реализует бизнес-логику `yaml-mcp-server` и `telegram-approver`.
- `codexctl` отвечает за:
  - корректную конфигурацию и вызов интеграций;
  - устойчивость orchestration flow;
  - безопасность и обработку секретов;
  - единые контракты портов и расширяемость.

## Целевая структура репозитория

- `cmd/codexctl/` — entrypoint.
- `internal/cli/` — transport слой.
- `internal/application/` — use-cases.
- `internal/core/` — инварианты и policy.
- `internal/adapters/orchestrator/` — реализации `Orchestrator`.
- `internal/adapters/repository/` — реализации `Repository`.
- `internal/adapters/state/` — backend хранения состояния.
- `internal/config/` — модель и загрузка `services.yaml`.
- `internal/prompt/` — рендер prompt/config.
- `.github/workflows/` — reusable workflows.
- `docs/design-guidelines/` — инженерный стандарт.

## Архитектурные запреты

- Смешивать parsing CLI и инфраструктурные side effects.
- Привязывать use-case слой к `client-go`/`go-github` типам.
- Дублировать инфраструктурные операции по разным командам.
- Встраивать знания о `yaml-mcp-server`/`telegram-approver` в core policy.
- Проектировать интерфейсы так, что они жестко фиксируют только Kubernetes/GitHub и блокируют Docker Compose/GitLab.
