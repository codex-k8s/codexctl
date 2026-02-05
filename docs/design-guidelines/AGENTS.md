# Design Guidelines

Стартовая точка для всех задач в `codexctl`.

Документация разделена по областям:

- `docs/design-guidelines/common/` — общие архитектурные и процессные правила.
- `docs/design-guidelines/go/` — правила проектирования и реализации Go-кода.

Важно: это эталонные требования, по которым репозиторий должен эволюционировать.
Если текущий код не совпадает с требованиями, новые изменения не должны усиливать расхождение.
Ключевая цель: перейти на портовую модель `Orchestrator`/`Repository` с базовыми SDK-реализациями
(`client-go` и `go-github`) и возможностью подключать `docker-compose`/`gitlab`.

Перед PR:

- `docs/design-guidelines/common/check_list.md`
- и, если есть Go-изменения:
  - `docs/design-guidelines/go/check_list.md`
