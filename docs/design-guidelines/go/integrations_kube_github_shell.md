# Интеграции: Kubernetes, GitHub, Shell

## Обязательная цель реализации

- Kubernetes-интеграция реализуется через `k8s.io/client-go`.
- GitHub-интеграция реализуется через `github.com/google/go-github`.
- Shell-вызовы (`kubectl`, `gh`) считаются временным техническим мостом и должны вытесняться SDK-адаптерами.

## Kubernetes (`client-go`)

Требования:

- Конфигурация клиента:
  - in-cluster: `rest.InClusterConfig()`;
  - out-of-cluster: `clientcmd.BuildConfigFromFlags()`.
- Настройка `QPS/Burst/Timeout` на уровне `rest.Config`.
- Весь Kubernetes доступ инкапсулирован в adapter реализации `Orchestrator`.
- Ошибки Kubernetes нормализуются в доменно-понятные ошибки orchestration слоя.

## GitHub (`go-github`)

Требования:

- Клиент создается через `github.NewClient(...)`.
- Аутентификация по токену через `WithAuthToken(...)` (или эквивалентный безопасный транспорт).
- Все вызовы идут с `context.Context` и явными deadline/timeout.
- Весь GitHub доступ инкапсулирован в adapter реализации `Repository`.

## Shell/process execution

Допускается только для:

- временной совместимости до полного перехода на SDK;
- операций, где SDK-эквивалент отсутствует и это явно зафиксировано.

Ограничения:

- shell-код только в adapter-слое;
- обязательные timeout, cwd, env и безопасная обработка аргументов;
- обязательный план миграции на SDK в техническом долге.

## Hooks

- Hooks остаются отдельным side-effect механизмом.
- Hooks не должны подменять Orchestrator/Repository портовую модель.
- Встроенные hooks должны иметь строгий контракт входов и ошибок.

## Preflight

- Проверки внешних бинарников остаются только для legacy-shell путей.
- Для SDK-путей preflight должен проверять конфигурацию и доступность API, а не наличие CLI-утилит.
