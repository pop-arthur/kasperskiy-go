# Инструкции для следующих сессий Codex

## Контекст и цель

Подготовить и затем постепенно реализовать Go backend тестового задания
Kaspersky: сведения об угрозе по MD5, GET /info/{md5}, POST /info,
PUT /info/{md5}. Перед реализацией расширить модель и OpenAPI.
Пересказ задания сохранён в docs/TASK_SOURCE.md, предоставленная OpenAPI —
в docs/backend/openapi.yaml; оригинальный документ задания не предоставлен. Рекомендации из пересказа не равны требованиям.

Bootstrap завершён; отдельной задачей добавлены SQL-миграция и запуск БД.
Реализованы доменные типы/валидация, интерфейс хранилища и pgx-адаптер.
Реализован ThreatService: GetByMD5/Create/Update с валидацией перед repository.
Реализован HTTP transport трёх операций с DTO, decoder, JSON-ошибками,
recovery/logging и тестами. cmd/api читает конфигурацию, проверяет
подключение/схему, собирает зависимости, запускает HTTP и завершает его
с drain перед закрытием пула. API добавлен в Compose.
Последующие изменения делать в объёме новой задачи.

## Обязательный порядок работы

1. Перед началом изучай AGENTS.md, docs/REQUIREMENTS.md, docs/ARCHITECTURE.md и docs/TASKS.md.
2. Не изменяй архитектуру без обоснования.
3. Не реализуй функциональность, не предусмотренную требованиями.
4. После выполнения задачи обновляй docs/TASKS.md.
5. При изменении архитектуры обновляй docs/ARCHITECTURE.md и docs/DECISIONS.md.
6. Не отмечай задачи выполненными без проверки результата.
7. Предпочитай простые, читаемые и идиоматичные решения.
8. Перед завершением запускай доступные проверки и сообщай результаты и ограничения.
9. Не удаляй исходные требования и не теряй ранее принятые решения.

## Стек и структура

Go 1.27.3; модуль kaspersky-go-test (реальный URL репозитория неизвестен).
Стандартная библиотека: net/http, log/slog, testing/httptest.
Конфигурация через окружение, DI через конструкторы. Единственная прямая
Go-зависимость — pgx/v5 v5.12.0; pgxpool использует его транзитивные зависимости.
Docker multi-stage, статический бинарник, непривилегированный runtime.
БД v1 принята в docs/database/README.md (ADR-006): PostgreSQL, одна таблица
threats, lowercase MD5, description и серверные created_at/updated_at.
Контракт API 1.1.0 принят в docs/backend/openapi.yaml (ADR-007); исходник
сохранён в openapi.original.yaml. Есть SQL-миграция и Compose для БД;
Go-репозиторий, HTTP API и сервер реализованы. Используются PostgreSQL 18.6,
pgx/v5 v5.12.0 и golang-migrate v4.20.1 (ADR-008). Драйвер добавлен в go.mod
с адаптером; мигратор запускается отдельным контейнером.
Контракт repository layer описан в docs/backend/REPOSITORY.md (ADR-009):
GetByMD5/Create/Update, интерфейс у usecase, pgx-адаптер в repository/postgres;
Go-интерфейс, типы и адаптер созданы; детали реализации — ADR-010. Перед реализацией репозитория прочитать
этот контракт. Спецификация сервисного слоя — docs/backend/SERVICE.md
(ADR-011): сервис равен usecase, сам вызывает ParseMD5/Validate перед
repository. ThreatService реализован и проверен unit/integration tests.
Перед изменениями читать спецификацию. HTTP-слой спроектирован в
docs/backend/HTTP.md (ADR-012), реализован в internal/transport/http.
Перед изменениями читать HTTP.md: DTO/обязательность/null/точные имена проверяет transport,
доменную валидацию вызывает сервис. Java/Spring из пересказа не применять.

Runtime описан в docs/backend/RUNTIME.md (ADR-013). cmd/api управляет
пулом и сервером; миграции не выполняет. Требуется версия схемы 1, dirty=false.
Перед изменением конфигурации/lifecycle читать RUNTIME.md. docs содержит требования, архитектуру, ADR, задачи
и источник. Makefile/Dockerfile обеспечивают сборку и запуск каркаса.

internal/domain — MD5, ThreatData, Threat, валидация и предметные ошибки;
internal/usecase — ThreatRepository и ThreatService с тремя сценариями и unit tests;
internal/repository/postgres — адаптер трёх операций с caller-owned pgxpool;
tests/integration — SQL/Compose и Go-репозиторий с реальной PostgreSQL.
internal/transport/http (httptransport) — handlers/router, DTO/decoder,
response/error mapping и recovery/logging; зависимости только stdlib/domain.
internal/config — Load() окружения с валидацией без dotenv. Создавать эти директории
только вместе с содержательным кодом. Не добавлять .gitkeep и пустые слои.

## Правила кода и архитектуры

- gofmt, короткие осмысленные имена, небольшие функции и пакеты без циклов.
- Transport → usecase → domain; адаптер хранения зависит от внутренних типов.
  Domain/usecase не зависят от HTTP, SQL, окружения и конкретного хранилища.
- Передавать зависимости через конструкторы, context.Context первым аргументом
  операций с вводом/выводом. Не хранить context в структурах.
- Интерфейсы определять у потребителя; не вводить универсальные абстракции,
  DI-контейнеры, generic repository и микросервисы для трёх операций.
- Не добавлять зависимость без документированного обоснования и оценки stdlib.
- Сохранять обратную совместимость API: пути, схемы, коды и семантику.
  Использовать актуальную OpenAPI; изменения контракта фиксировать явно.
- Не добавлять endpoints, сервисы или безопасность на основании одних предложений.

## Ошибки и логи

Возвращать ошибки явно; оборачивать с %w, сопоставлять errors.Is/As.
Ожидаемые ошибки не panic. Предметные ошибки отделять от HTTP-кодов;
transport отвечает за JSON message/details и 400/404/409/500 по контракту.
Не раскрывать клиенту технические детали. JSON slog с явной передачей
логгера, один лог ошибки на границе; без секретов и сырых пользовательских тел.
Ошибки конфигурации выявлять до запуска сервера.

## Проверки и тесты

Unit tests писать рядом с проверяемым кодом, testing/httptest; покрывать
некорректный MD5/тело, отсутствие записи, дубликаты, обновление и ошибки
по утверждённому контракту. Integration tests — реальное выбранное хранилище,
изолированные данные и отдельная документированная команда.
Не создавать фиктивных тестов и не заявлять покрытие при [no test files].

Команды каркаса (Go 1.27.3; старой Go нужен GOTOOLCHAIN=auto и сеть):

- go fmt ./..., go vet ./..., go test ./...; make check — эти три проверки.
- make build — bin/api; make run — локальный сервер; нужны экспортированные настройки БД и применённая миграция.
- make fmt, make vet, make test — отдельные проверки.
- make docker-build — сборка образа; make docker-run — алиас make up.
- make up — db-up → migrate-up → Compose API; make down сохраняет том; make logs — логи API.
- make test-runtime — реальный бинарник и Compose в изолированном проекте: startup, сигналы и persistence.

Команды БД: cp .env.example .env (если .env ещё нет), make db-up,
make migrate-up, make migrate-version, make db-shell, make db-down.
make migrate-down откатывает одну миграцию и удаляет её данные; выполнять
только по явной задаче. db-down сохраняет том. make test-db запускает
проверку схемы в отдельном случайно названном Compose-проекте и удаляет
только его том; нужны Docker Compose и Python 3. make test-repository
использует тот же изолированный запуск, применяет миграцию и выполняет
Go integration tests repository, service и HTTP с build tag integration и race detector.
Предпочитать make test-integration; make test-repository сохранён как алиас. Обычный
go test ./... не обращается к БД. Не запускать эти integration tests
на рабочей БД: они создают тестовые данные и временный индекс.

LOG_LEVEL по умолчанию INFO. Go .env автоматически не читает; Compose читает.
POSTGRES_PASSWORD обязателен, не логировать конфигурацию/DSN. HTTP_ADDR
локально 127.0.0.1:8080, в Compose 0.0.0.0:8080; API_PORT меняет публикацию.
На SIGTERM сначала Shutdown с отдельным контекстом, затем закрытие пула;
не связывать request BaseContext с сигналом. При timeout отменить запросы
и закрыть HTTP принудительно. Exit 0 штатный, ошибки startup/shutdown — 1.
Compose stop_grace_period 30s должен превышать HTTP_SHUTDOWN_TIMEOUT (default 10s).
PostgreSQL 18 хранит данные в томе на /var/lib/postgresql.
SQL в migrations — источник схемы; применённые миграции не менять,
изменения схемы оформлять новой парой up/down.
Результаты фактических проверок Bootstrap фиксируются в docs/TASKS.md.
