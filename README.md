# Kaspersky Go test

REST-сервис сведений о компьютерных угрозах по MD5:
GET /info/{md5}, POST /info, PUT /info/{md5}.
Доступ защищён Bearer API-ключом и rate limit по IP.
Реализованы доменная валидация, сервисный слой, PostgreSQL repository,
HTTP transport, конфигурация и корректное завершение процесса.

Go 1.27.3, стандартные net/http и slog, native pgx/v5 v5.12.0,
PostgreSQL 18.6, golang-migrate v4.20.1 и cleanenv v1.5.0. Без ORM, HTTP framework и DI-контейнера.
Код SQL-доступа генерируется sqlc v1.31.1, HTTP-модели — oapi-codegen v2.8.2.
Генераторы и golangci-lint v2.15.0 используются только при разработке;
новых зависимостей исполняемого приложения они не добавляют.

## Быстрый запуск через Docker Compose

Нужны Docker Engine, Compose v2 с up --wait и make. Если .env ещё нет:

```sh
cp .env.example .env
# Сгенерируйте ключ и запишите результат в API_KEY в .env:
openssl rand -hex 32
```

После записи `API_KEY` в `.env` запустите сервис:

```sh
make up
make logs
```

После смены `API_KEY` в `.env` снова выполните `make up`: Compose пересоздаст
API-контейнер с новым ключом. Обычный `docker compose restart api` не применяет
изменения окружения. Клиентам также нужно передавать новое значение ключа.
Сам ключ не следует записывать в README или коммитить в Git.

make up поднимает БД, явно применяет SQL-миграции, собирает/запускает API
и ожидает успешной проверки готовности контейнера. При ошибке startup или
истечении ожидания команда завершается с ненулевым кодом.
Бюджет ожидания по умолчанию — 30 секунд; его можно изменить:
`make API_WAIT_TIMEOUT=60 up`.
В логах "HTTP server listening" означает открытие listener после проверки БД.
По умолчанию API доступен на http://127.0.0.1:8080, PostgreSQL — 127.0.0.1:5432.
API_PORT меняет публикацию API; POSTGRES_PORT — публикацию БД.
Внутри Compose используются адреса 0.0.0.0:8080 и postgres:5432.
Если порт хоста занят другим проектом, задайте свободный порт в `.env`, например
`POSTGRES_PORT=5433` или `API_PORT=8081`, и повторите `make up`.
Менять внутренний порт PostgreSQL в Compose для этого не нужно.

```sh
make down
```

Контейнеры и сеть удаляются, постоянный том PostgreSQL сохраняется.
Повторный make up сохраняет данные. POSTGRES_DB/USER/PASSWORD применяются
образом при первичной инициализации тома; изменение .env само по себе не
меняет уже созданные роли/пароли. Образ API — scratch, UID/GID 65532,
статический бинарник и CA bundle. stop_grace_period — 30s; он должен быть
больше настроенного HTTP_SHUTDOWN_TIMEOUT.

Проверка готовности выполняется самим бинарником: `/api healthcheck` читает
HTTP_ADDR и вызывает GET /readyz через loopback. Только JSON 200 со статусом
ready означает готовность. Проверка PostgreSQL имеет бюджет
min(POSTGRES_OPERATION_TIMEOUT, 1s), общий бюджет CLI — 2s. Запросы readiness
не требуют ключа и не расходуют rate limit. Внешние обращения к /readyz
получают JSON 404. Compose проверяет готовность каждые 2 секунды.

## Примеры API

Экспортируйте API_KEY с тем же значением, которое задано серверу
(Go-процесс и shell автоматически .env не загружают).

```sh
curl -i -X POST http://127.0.0.1:8080/info \
  -H "Authorization: Bearer $API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"md5":"d41d8cd98f00b204e9800998ecf8427e","name":"Trojan.Win32.Generic","description":"Example threat"}'

curl -i -H "Authorization: Bearer $API_KEY" \
  http://127.0.0.1:8080/info/d41d8cd98f00b204e9800998ecf8427e

curl -i -X PUT http://127.0.0.1:8080/info/d41d8cd98f00b204e9800998ecf8427e \
  -H "Authorization: Bearer $API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Trojan.Win32.Updated"}'
```

Успешные POST/GET/PUT возвращают 201/200/200. PUT заменяет name/description;
отсутствующее description удаляет описание. Ошибки — JSON message/details:
400, 401, 404, 409, 429, 500. MD5 нормализуется в lowercase; пустое name допустимо
по принятому контракту. Даты назначает БД и возвращает в UTC.

## Локальный Go-процесс

Нужны Go 1.27.3, make и подготовленная PostgreSQL. Go может автоматически
скачать toolchain через GOTOOLCHAIN=auto; для первой сборки нужна сеть.
Go-процесс автоматически .env не загружает — экспортируйте переменные.
Compose читает .env самостоятельно. Локальный пароль/порт должны совпадать с БД:

```sh
make db-up
make migrate-up
API_KEY="$API_KEY" POSTGRES_PASSWORD=local_development_only make run
```

Если API уже запущен через Compose, задайте другой HTTP_ADDR, например
127.0.0.1:8081. make build создаёт bin/api; его можно запускать напрямую
с теми же переменными окружения. При startup проверяется подключение через Ping.
Приложение не проверяет версию/dirty-флаг миграций и наличие таблиц и само миграции
не выполняет. До запуска подготовьте схему через make migrate-up или make up.
SIGINT/SIGTERM запускают drain запросов, затем закрывают пул. Штатный exit 0;
ошибка startup или истечение shutdown timeout — exit 1.

## Конфигурация

| Переменная | Default | Проверка/назначение |
| --- | --- | --- |
| API_KEY | Нет | Обязательный секрет: 32–256 ASCII символов A–Z, a–z, 0–9, `_`, `-` |
| HTTP_RATE_LIMIT_RPS | 10 | Скорость восстановления токенов на IP, целое 1–2147483647 |
| HTTP_RATE_LIMIT_BURST | 20 | Ёмкость bucket на IP, целое 1–2147483647 |
| HTTP_RATE_LIMIT_MAX_CLIENTS | 10000 | Максимум хранимых IP, целое 1–2147483647 |
| LOG_LEVEL | INFO | Синтаксис slog, включая DEBUG+2 |
| HTTP_ADDR | 127.0.0.1:8080 | TCP host:port; числовой порт 0–65535, 0 выбирает свободный порт |
| HTTP_READ_HEADER_TIMEOUT | 5s | Положительная Go duration |
| HTTP_READ_TIMEOUT | 10s | Положительная Go duration |
| HTTP_WRITE_TIMEOUT | 15s | Положительная Go duration |
| HTTP_IDLE_TIMEOUT | 60s | Положительная Go duration |
| HTTP_SHUTDOWN_TIMEOUT | 10s | Положительная Go duration для drain |
| HTTP_MAX_BODY_BYTES | 1048576 | Положительное int64, лимит POST/PUT |
| POSTGRES_HOST | 127.0.0.1 | Непустой TCP host; IPv6 без квадратных скобок |
| POSTGRES_PORT | 5432 | 1–65535 |
| POSTGRES_DB | threats | Непустая строка |
| POSTGRES_USER | threats | Непустая строка |
| POSTGRES_PASSWORD | Нет | Обязательная непустая строка, без trim |
| POSTGRES_SSLMODE | disable | disable/allow/prefer/require/verify-ca/verify-full |
| POSTGRES_MAX_CONNS | 10 | 1–2147483647; минимум пула остаётся default pgxpool |
| POSTGRES_CONNECT_TIMEOUT | 5s | Положительная duration: соединение и общий startup budget |
| POSTGRES_OPERATION_TIMEOUT | 5s | Положительная duration: единый бюджет ожидания пула, выполнения SQL и чтения результата |

Default используется только при отсутствии переменной. Явно пустые,
некорректные или содержащие NUL значения отклоняются. Ошибка содержит имя
переменной, не её значение. Config содержит API_KEY и пароль; не логировать
структуру целиком. URL подключения строится net/url с UserPassword;
пароль с @, :, /, ?, #, %, пробелами не требует ручного экранирования.
URL не выводится в логи. Ошибки разбора pgx-конфигурации возвращаются без
сырой строки подключения. В runtime image добавлен системный CA bundle.

| Переменная Compose | Default | Назначение |
| --- | --- | --- |
| API_PORT | 8080 | Порт публикации API на хосте |

HTTP_ADDR допускает порт 0: фактический адрес выводится в логи.
HTTP-таймауты управляют чтением, записью и keep-alive в net/http,
а не временем выполнения сервисной операции.

## Жизненный цикл

Конфигурация читается из окружения библиотекой cleanenv через env-теги и затем
проверяется на ограничения приложения. Пустые значения не заменяются defaults;
ошибки не раскрывают значения переменных. Автоматической загрузки .env нет.
Затем pgxpool выполняет Ping в контексте POSTGRES_CONNECT_TIMEOUT, собираются
repository → ThreatService → HTTP handler и открывается listener.

Миграции — ответственность отдельного шага развёртывания: make up останавливается
при ошибке мигратора. Бинарник не читает schema_migrations и не выполняет DDL.
Если запустить его с доступной PostgreSQL без таблицы threats, startup и локальный
/readyz пройдут, а бизнес-запрос вернёт JSON 500 без технических деталей.
Readiness подтверждает соединение с БД, но не совместимость её схемы.

Контексты запросов независимы от контекста SIGINT/SIGTERM. Shutdown с отдельным
контекстом закрывает listener и даёт текущим запросам завершиться в пределах
HTTP_SHUTDOWN_TIMEOUT. При истечении лимита контексты запросов отменяются,
HTTP-соединения закрываются принудительно. Пул закрывается после остановки HTTP.
Штатная остановка, включая отмену запуска сигналом, возвращает exit 0;
ошибки конфигурации, подключения к БД, listener или завершения — exit 1.

Логи структурированы в JSON через slog. Логи запросов и lifecycle идут в stdout,
фатальные ошибки — один раз в stderr независимо от LOG_LEVEL. ErrorLog net/http
направлен в slog. Пароли, API-ключи, Authorization, DSN и сырые тела запросов не логируются;
технические детали внутренних ошибок не раскрываются клиенту.

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
- make fmt, make vet, make test — отдельные проверки; make lint — golangci-lint.
- make test-integration — Go integration tests с -race в отдельной PostgreSQL, включая timeout ожидания пула/блокировки и HTTP-ошибку deadline.
- make test-repository — совместимый алиас test-integration.
- make test-runtime — настоящий бинарник и Compose: startup failures, сигналы,
  HTTP, Docker build и сохранность данных после пересоздания контейнеров.
- make test-db — реальные миграции up/down/reapply, dirty-состояние и constraints.
- PostgreSQL для integration/DB/process-тестов поднимает Testcontainers Go v0.45.0
  с закреплённым образом, случайным портом и временными реквизитами. Нужен Docker;
  внешний TEST_DATABASE_URL не используется. Миграции выполняет одноразовый
  контейнер того же golang-migrate, что используется при развёртывании.
- make test-runtime включает отдельный Compose smoke test: дополнительно нужны
  Compose v2 и make. Проверяются настоящий Dockerfile, команды запуска и том.
- Все тесты написаны на Go. Теги integration/e2e отделяют Docker-тесты от unit;
  cleanup удаляет только собственные ресурсы с независимым контекстом.
- make db-shell — psql; make migrate-version — состояние миграции.
- make migrate-down откатывает одну миграцию и удаляет её данные.
- make docker-run — запуск через Compose, алиас make up.

## Архитектура

Зависимости передаются через конструкторы. HTTP transport вызывает usecase;
usecase использует доменные типы и интерфейс хранилища. PostgreSQL-адаптер
реализует этот интерфейс. Domain и usecase не зависят от HTTP, SQL или окружения.

| Директория | Ответственность |
| --- | --- |
| cmd/api/ | Точка входа, конфигурация процесса, сигналы и exit code |
| internal/app/ | Сборка зависимостей, подключение к БД, HTTP-сервер и lifecycle |
| internal/config/ | Чтение и проверка окружения |
| internal/domain/ | Модель, MD5, валидация и предметные ошибки |
| internal/usecase/ | ThreatService и интерфейс ThreatRepository |
| internal/repository/postgres/ | Адаптер хранилища, преобразование строк и ошибок БД в доменные типы |
| internal/repository/postgres/dbgen/ | Сгенерированные sqlc типы и запросы для pgx/v5 |
| internal/transport/http/ | Маршрутизация, DTO/JSON, коды ответа, recovery и logging |
| tests/integration/ | Repository/usecase/HTTP-тесты с настоящей PostgreSQL |
| tests/database/ | Миграции и ограничения БД |
| tests/runtime/ | Настоящий бинарник, startup, сигналы и HTTP |
| tests/compose/ | Docker Compose и сохранность тома |
| tests/testutil/ | Общие Docker/process fixtures только для тестов |
| migrations/ | SQL-миграции up/down; источник схемы для sqlc |
| db/queries/ | Исходные SQL-запросы хранилища |
| docs/backend/openapi.yaml | [Контракт API](docs/backend/openapi.yaml) |

Transport проверяет форму JSON, наличие и тип полей. Сервис вызывает доменную
валидацию до обращения к repository: MD5 — ровно 32 ASCII hex-символа,
нормализуемых в нижний регистр; U+0000 в текстовых полях запрещён.
Пустые строки и пробелы сохраняются без trim и ограничений длины.
Известные JSON-ключи регистрозависимы, при повторении используется последнее
значение. Неизвестные поля игнорируются. null в известных строковых полях запрещён.

Repository предоставляет GetByMD5/Create/Update, по одному SQL-запросу на операцию.
Операции записи возвращают строки через RETURNING; предварительного чтения и upsert нет.
Отсутствие записи преобразуется в предметную ошибку, дубликат MD5 определяется
только по SQLSTATE 23505 и ограничению threats_pkey. Остальные ошибки сохраняют
причину для errors.Is/As. Отмена контекста записи не гарантирует откат на сервере;
автоматических повторов нет. Пулом владеет вызывающий код.

Каждый вызов repository получает дочерний контекст с
POSTGRES_OPERATION_TIMEOUT (по умолчанию 5s). Бюджет начинается при входе
в repository и включает получение соединения, SQL и чтение результата;
чтение HTTP-тела в него не входит. Более ранний deadline или отмена
родительского контекста сохраняют приоритет. POSTGRES_CONNECT_TIMEOUT
продолжает отдельно ограничивать подключение и startup.

При истечении бюджета операция возвращает обёрнутую ошибку deadline;
transport использует существующий JSON 500 без технических деталей.
Отмена клиентского контекста сохраняет поведение без попытки отправить ответ.
HTTP_WRITE_TIMEOUT независимо ограничивает запись HTTP-ответа: если он уже
истёк, доставка JSON не гарантируется. При настройке таймаутов нужен запас
на чтение запроса и отправку ответа. Deadline ограничивает ожидание приложения,
но не гарантирует откат записи на PostgreSQL. Успешный результат SQL не
подменяется поздней отменой контекста.

## Схема PostgreSQL и миграции

Источник схемы — [миграция 000001](migrations/000001_create_threats.up.sql).

| Поле public.threats | Тип | Ограничения и назначение |
| --- | --- | --- |
| md5 | text COLLATE "C" | NOT NULL, PRIMARY KEY threats_pkey; CHECK threats_md5_format_check: ^[a-f0-9]{32}$ |
| name | text | NOT NULL, не уникально; пустая строка допустима |
| description | text | NULL означает отсутствие, пустая строка — отдельное значение |
| created_at | timestamptz | NOT NULL DEFAULT CURRENT_TIMESTAMP; неизменно при PUT |
| updated_at | timestamptz | NOT NULL DEFAULT CURRENT_TIMESTAMP; обновляется SQL-запросом при PUT |

Единственный индекс — первичный ключ по MD5. Даты назначает БД, при создании
они равны; ответы возвращают их в UTC. updated_at не является версией записи
или гарантированно возрастающей меткой. Триггеров нет.

Миграции выполняются отдельным одноразовым контейнером golang-migrate:

```sh
make db-up
make migrate-up
make migrate-version
```

После применения миграций можно запускать API. make up выполняет эти шаги
последовательно перед сборкой и запуском API. Применённые миграции не меняются:
изменения схемы оформляются следующей парой up/down. make migrate-down
откатывает одну миграцию; текущий откат удаляет таблицу threats со всеми данными.
make db-down, как и make down, сохраняет том PostgreSQL.

## Контракт HTTP

| Метод и путь | Успех | Ошибки |
| --- | --- | --- |
| GET /info/{md5} | 200 | 400, 401, 404, 429, 500 |
| POST /info | 201 | 400, 401, 409, 429, 500 |
| PUT /info/{md5} | 200 | 400, 401, 404, 429, 500 |
| GET /readyz (loopback) | 200 | 404 (внешний клиент), 503 |

POST требует строковые md5 и name, PUT — name. description необязателен;
его отсутствие при PUT удаляет прежнее описание. Успешный ответ содержит
md5, name, created_at, updated_at; description опускается, если в БД NULL.
PUT не создаёт отсутствующую запись. Конкурентные обновления используют
семантику последней успешной записи без проверки версии.

POST/PUT принимают один JSON-объект с Content-Type application/json и
необязательным charset=utf-8. Некорректное тело, media type, кодировка или
превышение лимита тела дают 400. После успешной авторизации неизвестный путь даёт 404; неподдержанный
метод — 405 с Allow. HEAD возвращает 405 без тела. Все контрактные ошибки
содержат строковые message и details. Ошибки HTTP-парсинга до handler и
отключение клиента не гарантируют JSON-ответа. При отмене контекста
во время чтения незавершённого тела соединение явно прерывается через
ErrAbortHandler: сервер не отправляет неявный пустой 200. Полностью
полученные некорректные JSON-запросы по-прежнему получают JSON 400.

## Доступ и ограничение запросов

Все бизнес-маршруты требуют `Authorization: Bearer <API_KEY>`. Один ключ даёт
одинаковые права на GET/POST/PUT; пользователей и ролей нет. Регистр схемы
Bearer не важен, регистр токена важен. Повторные и неоднозначные заголовки
отклоняются. SHA-256 представления токенов сравниваются за постоянное время.
Без правильного ключа возвращается JSON 401 с WWW-Authenticate: Bearer,
в том числе для неизвестных путей и неподдержанных методов.

Перед авторизацией применяется token bucket по IP непосредственного TCP-клиента
из RemoteAddr. Forwarded и X-Forwarded-For игнорируются, IPv4-mapped IPv6
нормализуется к IPv4. Неверные ключи тоже расходуют бюджет. При исчерпании —
JSON 429 с Retry-After в целых секундах. По умолчанию доступны 20 запросов сразу,
после чего восстанавливаются 10 токенов/с. На каждом запросе расходуется один токен.

Таблица IP ограничена HTTP_RATE_LIMIT_MAX_CLIENTS. Записи удаляются после пяти
минут бездействия; очистка запускается запросами раз в минуту и при заполнении.
Если места нет, новый IP получает 429 с Retry-After: 60. Фоновых goroutine нет.
Лимиты локальны для процесса, сбрасываются при перезапуске; клиенты за одним NAT
или proxy делят бюджет. Поддержка доверенных proxy отдельно не реализована.

Локальный /readyz обрабатывается до limiter и авторизации. Доступ определяется
только реальным loopback-адресом соединения. Не направляйте внешние запросы
к нему через локальный reverse proxy: для приложения такой proxy — локальный клиент.
Изменение ключа и лимитов не требует миграции БД;
ключ и настройки применяются при перезапуске процесса. Для смены ключа обновите
окружение сервера и клиентов и пересоздайте API-контейнер.

TLS HTTP здесь не настроен: при внешнем размещении передавайте Bearer-токен
только через HTTPS, завершаемый доверенным reverse proxy. Публикация Compose
по умолчанию ограничена localhost. Не коммитьте .env и рабочие ключи.

## Документация Go API

GoDoc описывает контракты пакетов, публичных типов и методов, ограничения
конструкторов, предметные ошибки, владение ресурсами и семантику отмены записей.
Просмотр из корня проекта:

```sh
go doc ./internal/domain
go doc ./internal/usecase ThreatRepository
go doc ./internal/config Load
go doc ./internal/transport/http NewHandler
```

Сгенерированные модели и SQL-запросы документируются генераторами;
их комментарии вручную не редактируются.

## Генерация и CI

[OpenAPI](docs/backend/openapi.yaml) — единственный источник HTTP-моделей.
[oapi-codegen](https://github.com/oapi-codegen/oapi-codegen) генерирует
internal/transport/http/types.gen.go по oapi-codegen.yaml. Ручных копий DTO нет.
Маршрутизатор и декодер остаются в HTTP-адаптере: они сохраняют контрактные
коды ошибок, проверку регистра ключей и различие отсутствующего поля и null.
Сгенерированные методы JSON не заменяют валидацию входящего запроса.

[sqlc](https://docs.sqlc.dev/en/latest/reference/config.html) читает схему из
migrations/ и запросы из db/queries/ по sqlc.yaml, создавая пакет dbgen.
Этот пакет используется только PostgreSQL-адаптером; домен и usecase
не зависят от сгенерированных SQL- или HTTP-моделей. Схему подготавливает
отдельный шаг миграций; startup приложения проверяет только подключение.

```sh
make generate        # SQL-доступ и HTTP-модели
make generate-sql    # Только sqlc
make generate-http   # Только oapi-codegen
make lint
```

Версии инструментов закреплены в Makefile. Для их первого запуска нужна сеть.
Сгенерированные файлы хранятся в Git и не редактируются вручную; обычная
сборка, включая Docker, генераторы не запускает.

.github/workflows/ci.yaml проверяет воспроизводимость генерации, gofmt,
сборку, vet, unit tests с race detector и golangci-lint. Отдельная задача
выполняет Go-проверки PostgreSQL через Testcontainers, процесса и Compose.
Тесты создают изолированные ресурсы и удаляют только их.
Настройки линтера находятся в .golangci.yml.
