# Пакетный дизайн Go-кода (эталон)

## Цель

Задать целевую структуру и контракты, по которым должна быть рефакторена реализация `codexctl`.

## Обязательные порты

### `Orchestrator`

Назначение: управление жизненным циклом окружения независимо от платформы.

Минимальный контракт (ориентир):

```go
type Orchestrator interface {
    Apply(ctx context.Context, req ApplyRequest) error
    Delete(ctx context.Context, req DeleteRequest) error
    WaitReady(ctx context.Context, req WaitRequest) error
    Status(ctx context.Context, req StatusRequest) (StatusResult, error)
}
```

### `Repository`

Назначение: операции с hosting provider (issues, PR, reviews, branches).

Минимальный контракт (ориентир):

```go
type Repository interface {
    GetIssue(ctx context.Context, number int) (Issue, error)
    GetPullRequest(ctx context.Context, number int) (PullRequest, error)
    FindPullRequestByBranch(ctx context.Context, branch string) (int, error)
    CreateComment(ctx context.Context, target CommentTarget, body string) error
    CloseIssue(ctx context.Context, number int) error
    DeleteBranch(ctx context.Context, branch string) error
}
```

Контракты можно расширять, но только при реальной потребности use-case слоя.

## Целевые зоны кода

1. `internal/cli`
   - transport и user I/O;
   - mapping флагов/env в команды use-case.
2. `internal/application`
   - use-case orchestration;
   - зависимости только от портов.
3. `internal/core`
   - policy, инварианты, типы сценариев;
   - без side effects.
4. `internal/adapters/orchestrator/*`
   - реализации `Orchestrator`:
     - `kubernetes` (на `client-go`);
     - `dockercompose` (future).
5. `internal/adapters/repository/*`
   - реализации `Repository`:
     - `github` (на `go-github`);
     - `gitlab` (future).

## Composition root

- Сборка конкретных реализаций выполняется в одном месте (bootstrap/composition root).
- Выбор backend/provider конфигурируется явно, без скрытых глобальных переключателей.
- Use-case код не должен знать, какой адаптер подключен.

## Запреты

- SDK-типы `client-go`/`go-github` в `application` и `core`.
- Прямые вызовы `kubectl`/`gh` из use-case слоя.
- Платформо-специфичные поля в портовых DTO, если они не универсальны.
- God-интерфейсы "на всё" вместо сценарно-ориентированных портов.
