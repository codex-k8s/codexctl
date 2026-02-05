# Внешние зависимости и границы

## Цель

Зафиксировать внешние зависимости `codexctl`, их роли и архитектурные границы.

## Основные зависимости платформы

1. Kubernetes API
   - роль: оркестрация окружений;
   - базовая библиотека: `k8s.io/client-go`.
2. Git hosting API
   - роль: работа с issue/pr/review/branch;
   - базовая библиотека: `github.com/google/go-github`.

Это базовые реализации для портов:

- `Orchestrator` -> Kubernetes (`client-go`)
- `Repository` -> GitHub (`go-github`)

## Сопряженные публичные проекты

1. `yaml-mcp-server`
   - роль: MCP-gateway и approval-chain.
2. `telegram-approver`
   - роль: approval через Telegram и callback в MCP-контур.

`codexctl` не дублирует их логику и не встраивает их внутренние детали в core.

## Правила границ

- Use-case слой знает только порты `Orchestrator`/`Repository`.
- SDK-типы (`client-go`, `go-github`) не выходят за пределы adapter-слоя.
- Секреты передаются только через env/secret-management.
- Конфигурация интеграций остается декларативной и проверяемой.

## Расширяемость

Контракты должны позволять подключить альтернативные адаптеры:

- `DockerComposeOrchestrator`
- `GitLabRepository`

без переписывания use-case логики.

## Политика изменений

При изменении внешнего контракта:

1. Обновить документацию и примеры.
2. Добавить/обновить smoke/integration checks.
3. Зафиксировать миграцию для пользователей.
