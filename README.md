# Kaspersky Go test — Bootstrap

Основа Go REST-сервиса для сведений о компьютерных угрозах по MD5.
Будущий API: GET /info/{md5}, POST /info, PUT /info/{md5}.
Сейчас создан только каркас: проверка LOG_LEVEL, JSON-лог и выход.
Сервер, эндпоинты, бизнес-логика и Go-адаптер хранения ещё не реализованы.
Добавлены PostgreSQL в Compose, SQL-миграция таблицы и проверка инфраструктуры.

## Стек

Go 1.27.3 ([официальные релизы](https://go.dev/dl/)), стандартная библиотека,
log/slog, будущий net/http и testing/httptest. Внешних Go-зависимостей нет.
Прагматичная Clean Architecture, DI через конструкторы при добавлении слоёв.
PostgreSQL 18.6, будущий драйвер pgx/v5 v5.12.0, мигратор golang-migrate v4.20.1. Module path kasperski-go-test локальный: URL репозитория неизвестен.

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
├── migrations/
│   ├── 000001_create_threats.up.sql
│   └── 000001_create_threats.down.sql
├── tests/integration/
│   ├── check_database.py
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
└── go.mod
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
реализация адаптера ещё не начата.

Будущие internal/domain, usecase, repository, transport/http и config
будут созданы вместе с кодом. tests/integration уже содержит проверку БД. Пустых пакетов нет.

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
доступны отдельно. Сборка создаёт bin/api. Тестовых файлов Go пока нет:
[no test files] — проверка компиляции пакета, не покрытие функциональности.

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

Подробности — в [docs/database/README.md](docs/database/README.md).

## Следующий этап

Модель БД, версии инфраструктуры и контракт API согласованы (ADR-006/007/008).
Далее реализовать предметную модель, pgx-репозиторий и первый сценарий
создания по docs/TASKS.md. Реализацию начинать по отдельной задаче.
