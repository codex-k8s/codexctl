# Архитектура проекта (эталон)

## Контекст

`codexctl` — это один Go CLI-оркестратор.

- Это не микросервисная система.
- Это не frontend-приложение.
- Основные интеграции: Kubernetes, GitHub, MCP-экосистема (`yaml-mcp-server`, `telegram-approver`).

## Целевая архитектурная модель

Модель: Ports and Adapters + четкие прикладные сценарии.

Слои и зависимости:

1. `cli` (входной транспорт)
   - парсинг команд/флагов/env;
   - вызов прикладных сценариев;
   - без инфраструктурной логики.
2. `application` (оркестрация сценариев)
   - сценарии `apply`, `ensure-ready`, `manage-env`, `prompt run`, `pr review-apply` и т.д.;
   - транзакционная и процессная логика;
   - зависит только от портов.
3. `core` (доменные политики CLI)
   - инварианты, правила именования, валидации и вычисления контекста;
   - не знает о `kubectl`, `gh`, shell и файловой системе.
4. `adapters` (инфраструктура)
   - Kubernetes adapter;
   - GitHub adapter;
   - shell/process adapter;
   - state backend adapter;
   - prompt/MCP config renderer adapter.

Правило зависимостей: только сверху вниз (`cli -> application -> core`),
адаптеры подключаются снаружи через интерфейсы.

## Границы ответственности

- `codexctl` не реализует бизнес-логику `yaml-mcp-server` и `telegram-approver`.
- `codexctl` отвечает за:
  - корректную конфигурацию интеграции;
  - корректные вызовы внешних контрактов;
  - безопасную обработку секретов и таймаутов;
  - устойчивость orchestration flow.

## Целевая структура репозитория

- `cmd/codexctl/` — тонкий entrypoint.
- `internal/cli/` — команда/флаги/env binding.
- `internal/application/` — use-case сценарии (целевая зона рефакторинга).
- `internal/core/` — инварианты и политики (целевая зона рефакторинга).
- `internal/adapters/` — инфраструктурные адаптеры (целевая зона рефакторинга).
- `internal/config/` — модель и загрузка `services.yaml`.
- `internal/prompt/` — рендеринг prompt/config.
- `.github/workflows/` — reusable workflow definitions.
- `docs/design-guidelines/` — эталонные инженерные стандарты.

Допускается промежуточное состояние в текущем коде, но новые изменения
должны двигать код именно к этой модели.

## Архитектурные запреты

- Смешивать parsing CLI и инфраструктурные side effects в одном месте.
- Размазывать orchestration логику по helper-функциям без явного use-case.
- Дублировать одинаковые shell/kubectl/gh вызовы по разным командам.
- Встраивать знания о внешних проектах (`yaml-mcp-server`, `telegram-approver`) в core-слой.
- Передавать секреты через логируемые аргументы и небезопасные каналы.
