# Kaspersky Go test — Bootstrap

Основа Go REST-сервиса для сведений о компьютерных угрозах по MD5.
Будущий API: GET /info/{md5}, POST /info, PUT /info/{md5}.
Сейчас создан только каркас: проверка LOG_LEVEL, JSON-лог и выход.
Сервер, эндпоинты, бизнес-логика и хранилище ещё не реализованы.

## Стек

Go 1.27.3 ([официальные релизы](https://go.dev/dl/)), стандартная библиотека,
log/slog, будущий net/http и testing/httptest. Внешних Go-зависимостей нет.
Прагматичная Clean Architecture, DI через конструкторы при добавлении слоёв.
Для реализации выбрана PostgreSQL (пока не подключена). Module path kasperski-go-test локальный: URL репозитория неизвестен.

## Структура и контекст

```text
.
├── .dockerignore
├── .env.example
├── .gitignore
├── AGENTS.md
├── Dockerfile
├── Makefile
├── README.md
├── cmd/api/main.go
├── docs/
│   ├── backend/
│   │   ├── openapi.original.yaml
│   │   └── openapi.yaml
│   ├── database/README.md
│   ├── ARCHITECTURE.md
│   ├── DECISIONS.md
│   ├── REQUIREMENTS.md
│   ├── TASK_SOURCE.md
│   └── TASKS.md
└── go.mod
```

Начать следующую сессию с [AGENTS.md](AGENTS.md),
[требований](docs/REQUIREMENTS.md), [архитектуры](docs/ARCHITECTURE.md)
и [задач](docs/TASKS.md). Решения сохранены в [ADR](docs/DECISIONS.md),
предоставленное описание — в [источнике](docs/TASK_SOURCE.md).
Оригинального документа задания нет; OpenAPI сохранена в
[docs/backend/openapi.yaml](docs/backend/openapi.yaml), проект БД v1 — в
[docs/database/README.md](docs/database/README.md). Предложения из пересказа
отмечены отдельно от обязательного поведения.

Будущие internal/domain, usecase, repository, transport/http и config,
а также tests/integration будут созданы вместе с кодом. Пустых пакетов нет.

## Локальная сборка и проверки

Нужны Go 1.27.3 и make. С установленной более старой Go при GOTOOLCHAIN=auto
toolchain автоматически скачивается по go.mod; для первого запуска нужна сеть.
Глобально установленная Go не изменяется.

```sh
go version
go fmt ./...
go vet ./...
go test ./...
make build
make run
```

`make check` выполняет fmt/vet/test; `make fmt`, `make vet`, `make test`
доступны отдельно. Сборка создаёт bin/api. Сейчас тестовых файлов нет:
[no test files] — проверка компиляции пакета, не покрытие функциональности.

## Конфигурация и запуск каркаса

Единственная настройка — LOG_LEVEL, по умолчанию INFO.
Обычные значения DEBUG/INFO/WARN/ERROR; используется синтаксис stdlib slog.
.env.example содержит справочник, автоматической загрузки .env нет.

```sh
LOG_LEVEL=DEBUG make run
LOG_LEVEL=INFO ./bin/api
```

При INFO выводится JSON-сообщение о Bootstrap и процесс завершается с кодом 0.
При WARN/ERROR сообщение подавляется. Некорректный уровень даёт JSON-ошибку
в stderr и код 1 у бинарника. Порт не открывается.

## Docker

Нужен работающий Docker Engine и доступ к registry для первого скачивания образа.

```sh
make docker-build
make docker-run
# Передать переменную явно:
LOG_LEVEL=DEBUG make docker-run
```

Образ kasperski-go-test:local: multi-stage Go build → scratch, CGO выключен,
runtime UID/GID 65532. Контейнер выполняет тот же Bootstrap и завершается.
Compose отсутствует, поскольку внешних сервисов и HTTP-сервера пока нет.

## Следующий этап

Модель БД и контракт API 1.1.0 согласованы (ADR-006/007). Далее выбрать
версию PostgreSQL, драйвер и средство миграций, затем реализовать модель
и первый сценарий создания по docs/TASKS.md. Реализацию начинать по отдельной задаче.
