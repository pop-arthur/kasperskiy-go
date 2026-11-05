# Kaspersky Go test

Основа Go REST-сервиса для сведений о компьютерных угрозах по MD5.
Будущий API: GET /info/{md5}, POST /info, PUT /info/{md5}.
Реализованы доменная модель/валидация, PostgreSQL repository layer, миграция
и изолированные проверки на реальной БД. Сервер, эндпоинты и usecase-сценарии
ещё отсутствуют: cmd/api по-прежнему проверяет LOG_LEVEL, пишет JSON-лог и выходит.

## Стек

Go 1.27.3 ([официальные релизы](https://go.dev/dl/)), стандартная библиотека,
log/slog, будущий net/http и testing/httptest. Единственная прямая Go-зависимость
— pgx/v5 v5.12.0 (с его транзитивными зависимостями).
Прагматичная Clean Architecture, DI через конструкторы при добавлении слоёв.
PostgreSQL 18.6, драйвер pgx/v5 v5.12.0, мигратор golang-migrate v4.20.1. Module path kasperski-go-test локальный: URL репозитория неизвестен.

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
├── compose.yaml
├── internal/
│   ├── domain/                 # MD5, модель, валидация и unit tests
│   ├── usecase/                # Интерфейс ThreatRepository
│   └── repository/postgres/    # Конкретный pgx-адаптер
├── migrations/
│   ├── 000001_create_threats.up.sql
│   └── 000001_create_threats.down.sql
├── tests/integration/
│   ├── check_database.py
│   ├── repository_test.go
│   └── schema.sql
├── docs/
│   ├── backend/
│   │   ├── REPOSITORY.md
│   │   ├── openapi.original.yaml
│   │   └── openapi.yaml
│   ├── database/README.md
│   ├── ARCHITECTURE.md
│   ├── DECISIONS.md
│   ├── REQUIREMENTS.md
│   ├── TASK_SOURCE.md
│   └── TASKS.md
├── go.mod
└── go.sum
```

Начать следующую сессию с [AGENTS.md](AGENTS.md),
[требований](docs/REQUIREMENTS.md), [архитектуры](docs/ARCHITECTURE.md)
и [задач](docs/TASKS.md). Решения сохранены в [ADR](docs/DECISIONS.md),
предоставленное описание — в [источнике](docs/TASK_SOURCE.md).
Оригинального документа задания нет; OpenAPI сохранена в
[docs/backend/openapi.yaml](docs/backend/openapi.yaml), проект БД v1 — в
[docs/database/README.md](docs/database/README.md). Предложения из пересказа
отмечены отдельно от обязательного поведения. Контракт repository layer
сохранён в [docs/backend/REPOSITORY.md](docs/backend/REPOSITORY.md);
адаптер реализован в internal/repository/postgres.

internal/domain содержит модель и валидацию, internal/usecase — интерфейс
хранилища, internal/repository/postgres — адаптер. HTTP transport, config
и usecase-сценарии будут добавлены с реализацией сервера. Пустых пакетов нет.

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
доступны отдельно. Сборка создаёт bin/api. Обычный go test ./... выполняет
unit tests доменной валидации и не требует БД. Тесты адаптера на реальной
PostgreSQL запускаются отдельно через make test-repository.

## Конфигурация и запуск каркаса

Единственная настройка Go-каркаса — LOG_LEVEL, по умолчанию INFO.
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

## PostgreSQL и миграции

Если .env ещё не создан:

```sh
cp .env.example .env
make db-up
make migrate-up
make migrate-version
```

Compose читает .env, Go-каркас его автоматически не загружает.
В примере заданы POSTGRES_DB, POSTGRES_USER, локальный POSTGRES_PASSWORD
и POSTGRES_PORT. БД доступна на 127.0.0.1:5432; порт можно изменить в .env.
Образы PostgreSQL и мигратора закреплены версиями и digest. Миграции
запускаются явно после готовности БД, API к ней пока не подключён.

- make db-shell — psql внутри контейнера.
- make db-down — остановка с сохранением постоянного тома.
- make migrate-up — применить миграции; повторный запуск безопасен.
- make migrate-down — откатить одну миграцию: удалит таблицу threats и её данные.
- make test-db — проверить миграции и сохранность данных в изолированном
  Compose-проекте (нужен Python 3, сторонних пакетов не требуется).
- make test-repository — 11 Go integration tests адаптера с -race в отдельной
  PostgreSQL с миграцией. Runner сам создаёт TEST_DATABASE_URL и удаляет
  только свой тестовый том; локальный .env и обычная БД не используются.

Подробности — в [docs/database/README.md](docs/database/README.md).

## Следующий этап

Модель БД, версии инфраструктуры и контракт API согласованы (ADR-006/007/008).
Доменная модель и pgx-репозиторий реализованы (ADR-010). Далее — первый
usecase-сценарий создания, затем конфигурация/жизненный цикл пула и HTTP
по docs/TASKS.md. Реализацию начинать по отдельной задаче.
