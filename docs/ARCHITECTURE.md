# Архитектура

## Текущее состояние

Один модуль kaspersky-go-test, PostgreSQL-адаптер на pgx/v5 v5.12.0.
cmd/api читает/проверяет окружение, создаёт JSON logger и пул, проверяет
соединение/схему, собирает repository → service → HTTP и запускает сервер.
[Runtime](backend/RUNTIME.md) и ADR-013 описывают startup/shutdown и defaults.
Docker: статический бинарник, scratch, UID/GID 65532 и системный CA bundle.

## Архитектура: реализованное и планируемое

Прагматичная Clean Architecture в одном сервисе. Создавать пакеты при
появлении реального кода; пустые слои и интерфейсы на вырост не нужны.

| Путь | Ответственность и статус |
| --- | --- |
| `cmd/api` | Composition root: чтение конфигурации, сборка зависимостей, жизненный цикл процесса |
| `internal/config` | Реализованы Load(), defaults, валидация HTTP/PostgreSQL и безопасная сборка URL |
| `internal/domain` | Реализовано: MD5, ThreatData, Threat, валидация и ошибки без HTTP/SQL |
| `internal/usecase` | Реализованы ThreatRepository и ThreatService: GetByMD5/Create/Update с валидацией и unit tests |
| `internal/repository/postgres` | Реализованы GetByMD5/Create/Update на pgxpool; уникальность MD5 обеспечивает БД |
| `internal/transport/http` | Реализованы маршруты, DTO/decoder/writer, ошибки, recovery/logging и httptest |
| `tests/integration` | Реализованы SQL/Compose и цепочка HTTP → service → repository → PostgreSQL |

Подпакеты handler/middleware и infrastructure создавать только при
необходимости. Сейчас их нет. Не размещать интерфейс репозитория в
отдельном абстрактном слое только ради структуры.

Зависимости: transport → usecase → domain; repository → domain и, если
нужно, типы usecase. Внутренние слои не импортируют HTTP, SQL, окружение или
конкретные адаптеры. cmd/api связывает реализации через конструкторы;
DI-контейнер и глобальные зависимости не нужны.

Предполагаемый поток: HTTP → декодирование/валидация DTO → usecase →
интерфейс хранилища → адаптер → результат → HTTP-ответ по OpenAPI.
Будущая маршрутизация — stdlib net/http, если контракт не потребует иного.
Весь поток собран в cmd/api; процесс/Compose проверены отдельно от httptest.

## Контракт repository layer

[Контракт repository v1](backend/REPOSITORY.md) реализован и проверен
на PostgreSQL 18.6 (ADR-010).
Три метода: GetByMD5, Create, Update. Интерфейс ThreatRepository принадлежит
internal/usecase; конкретный pgx-адаптер находится в internal/repository/postgres.
Вход — канонический domain.MD5 и изменяемые Name/Description без клиентских
дат; результат — полная domain.Threat. Ошибки отсутствия/дубликата —
ErrThreatNotFound/ErrThreatAlreadyExists, без HTTP и драйверных типов в сигнатурах.

Domain/usecase проверяют и нормализуют данные до вызова адаптера. Repository
сохраняет различие nil description и пустой строки, использует RETURNING
и даты БД, не делает upsert или предварительный Exists. Пул создаёт и закрывает
composition root; миграции остаются отдельным шагом (ADR-009).

## Контракт сервисного слоя

[Спецификация сервиса](backend/SERVICE.md) реализована в
internal/usecase/threat_service.go и проверена unit/integration tests.
Сервисный слой — это internal/usecase, дополнительный internal/service не нужен.
ThreatService получает ThreatRepository через конструктор и предоставляет
GetByMD5/Create/Update. Входной MD5 — сырая строка; сервис вызывает ParseMD5,
а при записи — ThreatData.Validate, затем один соответствующий метод repository.
Невалидные данные не доходят до хранилища. JSON/обязательность полей проверяет
transport; даты/NULL/уникальность обеспечивает repository. Сервис передаёт
ошибки без повторной обёртки, сохраняя их цепочку и errors.Is/As.
HTTP, retry, транзакции и жизненный цикл пула в сервис не добавляются.
Решение зафиксировано в ADR-011.

## HTTP-слой

[Спецификация HTTP](backend/HTTP.md) реализована и проверена (ADR-012).
Один пакет internal/transport/http (httptransport): router, handlers,
DTO, decoder, JSON writer/error mapping и recovery/logging. Локальный
интерфейс ThreatService совпадает с тремя методами usecase; зависимости
передаются NewHandler(service, logger, maxBodyBytes), без доступа к repository.

POST/PUT проверяют JSON/обязательность/null до вызова сервиса. Decoder
использует ограниченное тело и map[string]json.RawMessage: известные имена
case-sensitive, неизвестные поля игнорируются. Сервис остаётся единственным
местом вызова доменной валидации. DTO ответа отделены от domain; description
сохраняет nil/пустую строку, даты приводятся к UTC. Ошибки возвращаются
через единый JSON writer без утечки технических подробностей.

Проектные решения: default тела 1 MiB, JSON Content-Type/UTF-8, 400 для
ошибок формы/media type/размера, JSON fallback 404 и method 405 с Allow.
Это дополнение HTTP-проекта, не исходные требования ТЗ. Пути/статусы
трёх операций OpenAPI остаются прежними. Конфигурация сервера и wiring
реализованы в cmd/api и internal/config. Handlers подключены через
NewHandler, HTTP-порт открывается после успешной проверки БД/миграции.

## Конфигурация и логирование

internal/config.Load читает LOG_LEVEL, HTTP_* и POSTGRES_*; defaults
и правила описаны в RUNTIME.md и .env.example. Только unset получает
default; явно пустое значение — ошибка. Пароль обязателен и не обрезается.
Приложение .env не читает, Compose читает. Config не импортирует pgx: URL
строится через net/url, пул создаётся composition root. Ошибки конфигурации
завершают startup до открытия HTTP listener.
Fatal startup/shutdown пишется один раз JSON в stderr независимо от LOG_LEVEL;
остальные события — через настроенный stdout logger. ErrorLog net/http также
направлен в slog. Конфигурация/DSN целиком не логируются.
JSON structured logs через log/slog. Логгер передавать явно; будущие ошибки
запросов логировать один раз на границе, не выводить секреты/сырые тела.

## Ошибки (план)

Предметные ошибки отделять от технических, оборачивать через `%w`, проверять
errors.Is/As. Transport сопоставит их с 400/404/409/500 и JSON message/details
по уточнённому OpenAPI. Внутренние ошибки не раскрывать клиенту.
Конкурентные дубликаты обеспечивать средствами выбранного хранилища,
а не только предварительным поиском. Не использовать panic для ожидаемых ошибок.

## Тестирование

Unit-тесты Go проверяют domain, сервис и HTTP transport на ручных fake,
без БД. HTTP проверяется через ResponseRecorder и реальный httptest.Server
для HEAD, chunked/частичного ответа и прерывания потока.
make test-integration (алиас make test-repository) проверяет Go-адаптер
и цепочку HTTP → service → repository с реальной PostgreSQL, включая
гонки Create/Update, ошибки, NULL, UTC, context и deadline; включён -race.
Пул создаёт тест, миграцию применяет изолированный Compose-runner.
Есть make test-db: Python stdlib + Docker Compose проверяют реальные
up/down-миграции, ограничения и сохранность данных после пересоздания
контейнеров. Используется отдельный проект, порт и том; обычная БД не затрагивается.
Unit tests сервиса и transport реализованы через testing/httptest. Тестовые реализации
интерфейса писать вручную, когда это проще зависимости.
В tests/integration есть check_database.py, schema.sql, repository_test.go
service_test.go, http_test.go и runtime_checks.py.
Go integration tests включаются тегом integration и требуют отдельной
тестовой TEST_DATABASE_URL; make test-integration создаёт её автоматически.
HTTP integration tests проверяют все шесть кодов операций, в том числе
500 на закрытом пуле, duplicate, NULL и отсутствие upsert.
Они должны иметь документированный отдельный запуск и изоляцию данных;
обычный go test ./... не должен неожиданно требовать внешнюю БД.
Фиксировать ожидаемое поведение по контракту, не повторять реализацию в тестах.

## Проект хранения v1

Актуальный контракт API 1.1.0 сохранён в docs/backend/openapi.yaml, исходная
спецификация — в docs/backend/openapi.original.yaml.
[Схема БД v1](database/README.md) принята пользователем: PostgreSQL, одна
таблица threats, естественный ключ md5, нормализация регистра, description
и серверные даты (ADR-006/007). PUT полностью заменяет name/description;
отсутствующее description сбрасывается в NULL. Даты только серверные,
выдаются в ответах; неизвестные поля запросов игнорируются.
Таблица реализована миграцией 000001_create_threats, БД запускается
через Compose. Composition root владеет пулом: общий startup budget
включает Ping и проверку миграции 1/dirty=false/SELECT threats. DDL не
выполняется; при ошибке процесс завершается с рекомендацией make migrate-up.

## Инфраструктура и границы

PostgreSQL 18.6-bookworm и golang-migrate v4.20.1 закреплены версиями
и digest в compose.yaml (ADR-008). Compose запускает postgres, а migrate
доступен как одноразовый сервис профиля tools после service_healthy.
make db-up ждёт готовности БД; миграции применяются явно через make migrate-up.
Мигратор читает SQL из migrations (read-only), хранит версию и dirty-флаг
в служебной schema_migrations. Одна предметная таблица остаётся threats.

Именованный том postgres_data подключён к /var/lib/postgresql — корню
данных официального образа PostgreSQL 18. db-down не удаляет том.
Порт опубликован только на 127.0.0.1, по умолчанию 5432. БД/пользователь/пароль
и порт задаются окружением Compose; .env.example содержит локальные значения.
Мигратор получает имя БД, пользователя и пароль через PGDATABASE/PGUSER/PGPASSWORD,
чтобы специальные символы пароля не требовали URL-экранирования. sslmode=disable
используется только для локальной Compose-сети.

Адаптер использует native pgx/v5 v5.12.0 и pgxpool, без ORM.
Драйвер внесён в go.mod/go.sum. Конструктор принимает готовый ненулевой
пул; не создаёт подключения, не проверяет env и не закрывает пул. CLI мигратора
не встраивается в сервер и не является его Go-зависимостью.
API добавлен в Compose: 0.0.0.0:8080 → 127.0.0.1:API_PORT, БД postgres:5432.
make up запускает БД, мигратор и API последовательно. make down сохраняет том,
make logs показывает API; stop_grace_period=30s. Dockerfile копирует cmd/internal
и CA bundle, использует go.mod/go.sum и go mod download.

На сигнал Shutdown закрывает listener и ждёт запросы в отдельном контексте.
BaseContext запросов независим от сигнала; только по истечении shutdown timeout
запросы отменяются и HTTP закрывается принудительно. Пул закрывается после
остановки сервера. Нормальный exit 0, ошибка/timeout — exit 1.

make test-runtime проверяет настоящий процесс и контейнеры; make test-integration
продолжает проверять 13 Go integration tests. Миграции и OpenAPI не менялись.
