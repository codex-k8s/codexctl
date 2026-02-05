# Reusable Workflows (эталон)

## Общая модель

`codexctl` хранит reusable workflow definitions в `.github/workflows/*.yml`.

Проектные репозитории должны хранить только trigger-wrapper workflow,
которые вызывают reusable workflow из `codexctl` через `uses:`.

## Правила для reusable workflow в `codexctl`

- Каждый reusable workflow должен использовать `on: workflow_call`.
- Контракт secrets/inputs должен быть явным и минимально необходимым.
- Нельзя смешивать в одном workflow независимые бизнес-сценарии.
- Названия workflow и jobs должны отражать сценарий, а не реализацию.
- Повторяемые шаги должны быть унифицированы между workflow.

## Правила для wrapper workflow в проектах

- В wrapper остаются только триггеры и вызов `uses:`.
- Рекомендуемая фиксация версии `codexctl` reusable workflow: tag или commit SHA.
- `secrets: inherit` допускается только если это осознано и задокументировано.

## Контрактная стабильность

- Изменение интерфейса reusable workflow считается публичным контрактом.
- Breaking изменения допускаются только с миграционным описанием.
- README должен содержать актуальный пример wrapper-подключения.

## Безопасность

- Нельзя выводить секреты в логах workflow.
- Нельзя прокидывать токены через небезопасные аргументы shell, если есть безопасная альтернатива.
- Для критичных операций должны быть защитные условия запуска (trusted users, branch patterns, labels).
