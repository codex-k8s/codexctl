# Кросс-репо и кросс-платформенная совместимость

## Область

Совместимость между:

- `codexctl`
- `yaml-mcp-server`
- `telegram-approver`

и совместимость между провайдерами платформы:

- `Orchestrator`: Kubernetes (default) / Docker Compose (future)
- `Repository`: GitHub (default) / GitLab (future)

## Принципы

- Use-case сценарии не зависят от конкретного provider.
- Provider-специфичные различия локализуются в adapter-слое.
- Любая зависимость от недокументированного внешнего поведения запрещена.

## Контрактные точки

1. MCP endpoint и схема подключения серверов.
2. Формат переменных окружения для MCP/approval.
3. Timeout-модель для ручного approval.
4. Issue/PR/review/branch API-контракты для `Repository` порта.
5. Apply/Delete/Wait/Status контракты для `Orchestrator` порта.

## Политика совместимости реализаций

- Базовые реализации:
  - Kubernetes через `client-go`;
  - GitHub через `go-github`.
- Альтернативные реализации подключаются без изменения use-case слоя.
- При добавлении `docker-compose` или `gitlab`:
  - обновить compatibility matrix в документации;
  - добавить integration/smoke проверки для нового провайдера;
  - не ломать API существующих портов без миграционного слоя.

## Политика версий

- Внешние integration points фиксируются по версиям/референсам.
- Для workflow reuse использовать tag или commit SHA.
- Избегать плавающих зависимостей в критичных CI/CD сценариях.

## Процедура при breaking changes

1. Зафиксировать изменение и область влияния.
2. Добавить миграционный путь.
3. Обновить `README*` и `docs/design-guidelines/**`.
4. Обновить compatibility tests/smoke checks.
