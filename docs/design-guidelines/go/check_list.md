# Go чек-лист перед PR

## Архитектура

- Изменения двигают код к целевой Ports and Adapters модели.
- Use-case слой зависит от интерфейсов `Orchestrator`/`Repository`, а не от SDK типов.
- CLI, application/core и adapters не смешаны в одном модуле.

## Реализации по умолчанию

- Kubernetes-логика реализуется через `client-go` adapter.
- GitHub-логика реализуется через `go-github` adapter.
- Новый код не добавляет прямую зависимость use-case слоя от `kubectl`/`gh`.

## Расширяемость

- Контракты не блокируют добавление `docker-compose` и `gitlab` адаптеров.
- Provider-specific детали не протекают в общие DTO use-case слоя.

## Надежность

- Для внешних операций заданы timeout/retry/cancel semantics.
- Ошибки нормализуются и диагностируемы.
- Нет небезопасной shell-интерполяции пользовательского ввода.

## MCP и кросс-репо

- Изменения `codex.mcp.servers` валидно рендерятся в `config.toml`.
- Для approval-like tool задан адекватный `tool_timeout_sec`.
- Проверена совместимость с контрактами `yaml-mcp-server` и `telegram-approver`.

## Качество кода и документации

- Выполнены: `go fmt ./...`, `go vet ./...`, `go test ./...`.
- При изменениях публичного поведения обновлены `README.md` и `README_RU.md`.
- Документация в `docs/design-guidelines/**` актуализирована.
