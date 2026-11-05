# Kaspersky Go test

REST-сервис сведений о компьютерных угрозах по MD5:
GET /info/{md5}, POST /info, PUT /info/{md5}.
Реализованы доменная валидация, сервисный слой, PostgreSQL repository,
HTTP transport, конфигурация и корректное завершение процесса.

Go 1.27.3, стандартные net/http и slog, native pgx/v5 v5.12.0,
PostgreSQL 18.6 и golang-migrate v4.20.1. Без ORM, HTTP framework и DI-контейнера.
Module path kasperski-go-test локальный: реальный URL репозитория не предоставлен.

## Быстрый запуск через Docker Compose

Нужны Docker Engine, Compose v2 с up --wait и make. Если .env ещё нет:

```sh
cp .env.example .env
```

Затем:

```sh
make up
make logs
```

make up поднимает БД, явно применяет SQL-миграции и собирает/запускает API.
В логах "HTTP server listening" означает открытие listener после проверки БД.
По умолчанию API доступен на http://127.0.0.1:8080, PostgreSQL — 127.0.0.1:5432.
API_PORT меняет публикацию API; POSTGRES_PORT — публикацию БД.
Внутри Compose используются адреса 0.0.0.0:8080 и postgres:5432.

```sh
make down
```

Контейнеры и сеть удаляются, постоянный том PostgreSQL сохраняется.
Повторный make up сохраняет данные. POSTGRES_DB/USER/PASSWORD применяются
образом при первичной инициализации тома; изменение .env само по себе не
меняет уже созданные роли/пароли. Образ API — scratch, UID/GID 65532,
статический бинарник и CA bundle. stop_grace_period — 30s; он должен быть
больше настроенного HTTP_SHUTDOWN_TIMEOUT.

## Примеры API

```sh
curl -i -X POST http://127.0.0.1:8080/info \
  -H 'Content-Type: application/json' \
  -d '{"md5":"d41d8cd98f00b204e9800998ecf8427e","name":"Trojan.Win32.Generic","description":"Example threat"}'

curl -i http://127.0.0.1:8080/info/d41d8cd98f00b204e9800998ecf8427e

curl -i -X PUT http://127.0.0.1:8080/info/d41d8cd98f00b204e9800998ecf8427e \
  -H 'Content-Type: application/json' \
  -d '{"name":"Trojan.Win32.Updated"}'
```

Успешные POST/GET/PUT возвращают 201/200/200. PUT заменяет name/description;
отсутствующее description удаляет описание. Ошибки — JSON message/details:
400, 404, 409, 500. MD5 нормализуется в lowercase; пустое name допустимо
по принятому контракту. Даты назначает БД и возвращает в UTC.

## Локальный Go-процесс

Нужны Go 1.27.3, make и подготовленная PostgreSQL. Go может автоматически
скачать toolchain через GOTOOLCHAIN=auto; для первой сборки нужна сеть.
Go-процесс автоматически .env не загружает — экспортируйте переменные.
Compose читает .env самостоятельно. Локальный пароль/порт должны совпадать с БД:

```sh
make db-up
make migrate-up
POSTGRES_PASSWORD=local_development_only make run
```

Если API уже запущен через Compose, задайте другой HTTP_ADDR, например
127.0.0.1:8081. make build создаёт bin/api; его можно запускать напрямую
с теми же переменными окружения. При startup проверяются соединение,
версия миграции 1, dirty=false и таблица; приложение само миграции не выполняет.
SIGINT/SIGTERM запускают drain запросов, затем закрывают пул. Штатный exit 0;
ошибка startup или истечение shutdown timeout — exit 1.

Полная таблица переменных, defaults и lifecycle — [RUNTIME.md](docs/backend/RUNTIME.md).
Основные defaults: HTTP_ADDR=127.0.0.1:8080, body=1 MiB, таймауты header/read/write/
idle/shutdown=5s/10s/15s/60s/10s, pool max=10, connect/startup=5s.
POSTGRES_PASSWORD обязателен; .env.example содержит только локальный пример.

## Проверки и команды

```sh
make check
go test -race ./...
make test-integration
make test-runtime
make test-db
make build
make docker-build
```

- make check — fmt/vet/unit tests без БД.
- make test-integration — 13 Go integration tests с -race в отдельной PostgreSQL.
- make test-repository — совместимый алиас test-integration.
- make test-runtime — настоящий бинарник и Compose: startup failures, сигналы,
  HTTP, Docker build и сохранность данных после пересоздания контейнеров.
- make test-db — миграции, constraints и сохранность тома.
- Для тестов БД нужны Docker Compose и Python 3 без сторонних пакетов.
  Runner создаёт отдельные проект/порты/том и удаляет только их в finally.
- make db-shell — psql; make migrate-version — состояние миграции.
- make migrate-down откатывает одну миграцию и удаляет её данные.
- make docker-run — запуск через Compose, алиас make up.

## Структура и контекст

```text
cmd/api/                    composition root, startup и lifecycle
internal/config/            окружение и проверка настроек
internal/domain/            модель, MD5, валидация и ошибки
internal/usecase/           ThreatService и интерфейс repository
internal/repository/postgres/ pgx-адаптер
internal/transport/http/    router, DTO/JSON, middleware и ошибки
tests/integration/          SQL, Go integration, process и Compose smoke tests
migrations/                SQL up/down
docs/backend/              OpenAPI, контракты HTTP/service/repository/runtime
docs/database/             описание таблицы
```

Начать следующую сессию с [AGENTS.md](AGENTS.md), [требований](docs/REQUIREMENTS.md),
[архитектуры](docs/ARCHITECTURE.md) и [задач](docs/TASKS.md).
Причины выбора решений — [ADR](docs/DECISIONS.md).
Предоставленный пересказ задания сохранён в [TASK_SOURCE.md](docs/TASK_SOURCE.md),
исходная OpenAPI — [openapi.original.yaml](docs/backend/openapi.original.yaml),
актуальный контракт — [openapi.yaml](docs/backend/openapi.yaml).
Оригинальный документ задания Kaspersky пока не предоставлен.
