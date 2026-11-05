# Архитектура

## Реализовано на этапе Bootstrap

Один модуль `kasperski-go-test`; после Bootstrap добавлен pgx/v5 v5.12.0
для PostgreSQL-адаптера (ADR-010). `cmd/api/main.go`
проверяет LOG_LEVEL, создаёт JSON-логгер slog и завершает процесс с кодом 0.
Некорректный LOG_LEVEL даёт JSON-ошибку в stderr и код 1. HTTP-сервер не
запускается. При WARN/ERROR информационное сообщение подавляется.
Docker собирает статический бинарник и запускает его непривилегированным
пользователем в scratch. Сейчас контейнер также сразу завершается.

## Архитектура: реализованное и планируемое

Прагматичная Clean Architecture в одном сервисе. Создавать пакеты при
появлении реального кода; пустые слои и интерфейсы на вырост не нужны.

| Путь | Ответственность и статус |
| --- | --- |
| `cmd/api` | Composition root: чтение конфигурации, сборка зависимостей, жизненный цикл процесса |
| `internal/config` | Планируется: чтение и проверка окружения для сервера |
| `internal/domain` | Реализовано: MD5, ThreatData, Threat, валидация и ошибки без HTTP/SQL |
| `internal/usecase` | Реализованы ThreatRepository и ThreatService: GetByMD5/Create/Update с валидацией и unit tests |
| `internal/repository/postgres` | Реализованы GetByMD5/Create/Update на pgxpool; уникальность MD5 обеспечивает БД |
| `internal/transport/http` | Планируется: HTTP-маршрутизация, DTO и преобразование ошибок |
| `tests/integration` | Реализованы SQL/Compose, repository и service + PostgreSQL tests; HTTP tests планируются |

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
Хранение и usecase-сценарии реализованы; сервер и HTTP transport ещё отсутствуют.

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

## Конфигурация и логирование

Сейчас используется только LOG_LEVEL (по умолчанию INFO), поддерживаются
уровни стандартного slog, в примерах DEBUG/INFO/WARN/ERROR. `.env.example`
служит справочником; приложение автоматически .env не читает.
Параметры HTTP и хранилища добавлять вместе с их реализацией. Ошибки
конфигурации должны приводить к завершению до запуска сервера.
JSON structured logs через log/slog. Логгер передавать явно; будущие ошибки
запросов логировать один раз на границе, не выводить секреты/сырые тела.

## Ошибки (план)

Предметные ошибки отделять от технических, оборачивать через `%w`, проверять
errors.Is/As. Transport сопоставит их с 400/404/409/500 и JSON message/details
по уточнённому OpenAPI. Внутренние ошибки не раскрывать клиенту.
Конкурентные дубликаты обеспечивать средствами выбранного хранилища,
а не только предварительным поиском. Не использовать panic для ожидаемых ошибок.

## Тестирование

Unit-тесты Go проверяют domain.ParseMD5, ThreatData.Validate и все три
сценария ThreatService на ручном fake репозитория, без БД.
make test-repository проверяет Go-адаптер и цепочку service → repository
с реальной PostgreSQL, включая
гонки Create/Update, ошибки, NULL, UTC, context и deadline; включён -race.
Пул создаёт тест, миграцию применяет изолированный Compose-runner.
Есть make test-db: Python stdlib + Docker Compose проверяют реальные
up/down-миграции, ограничения и сохранность данных после пересоздания
контейнеров. Используется отдельный проект, порт и том; обычная БД не затрагивается.
Unit tests сервиса уже реализованы через testing; transport tests на
httptest будут добавлены с HTTP-слоем. Тестовые реализации
интерфейса писать вручную, когда это проще зависимости.
В tests/integration есть check_database.py, schema.sql, repository_test.go
и service_test.go.
Go integration tests включаются тегом integration и требуют отдельной
тестовой TEST_DATABASE_URL; make test-repository создаёт её автоматически.
Тесты цепочки HTTP → хранение, дубликатов и обновлений будут добавлены с API.
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
через Compose. Адаптер реализован; composition root пока не создаёт пул,
Go-приложение по-прежнему выполняет только Bootstrap.

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
API в Compose пока не добавлен: текущий бинарник сразу завершается. Dockerfile
копирует cmd/internal, использует go.mod/go.sum и go mod download.
Миграции и OpenAPI при реализации адаптера не изменялись.
