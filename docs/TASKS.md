# План работ

Обозначения: [ ] — не начато, [-] — в работе, [x] — выполнено.
Этапы после Bootstrap требуют отдельной задачи; текущая инициализация
не даёт указания начать бизнес-реализацию.

## 0. Bootstrap

- [x] Осмотреть директорию: исходных файлов задания не было; сохранить предоставленный затем пересказ в TASK_SOURCE.md.
- [x] Выделить требования, рекомендации и неизвестные условия; зафиксировать отсутствие оригинала/OpenAPI.
- [x] Выбрать локальное имя модуля и Go 1.27.3, без внешних Go-зависимостей.
- [x] Создать минимальную точку входа, конфигурацию логирования, Dockerfile и Makefile.
- [x] Сохранить инструкции агенту, архитектуру, ADR и README.
- [x] Проверить fmt/vet/test, сборку, запуск и Docker; записать фактические результаты.

## 1. Контракт до реализации

- [x] Сохранить предоставленную пользователем OpenAPI 3.0.0 в `docs/backend/openapi.yaml` без изменения содержимого; побайтовое совпадение с вложением проверено 2026-11-05. Ранее отмеченное отсутствие OpenAPI относится к моменту Bootstrap; сверка требований и расширение модели ещё не выполнены.

- [x] Получить OpenAPI; сопоставить пути, статусы, схемы и обязательность полей с REQUIREMENTS.md.
- [ ] Получить оригинальный документ задания и сверить дополнительные требования; его отсутствие не заменять пересказом.
- [x] Выбрать дополнительные поля модели, допустимые значения, default и семантику времени; записать решение в ADR-006/007.
- [x] Уточнить валидацию, регистр MD5, полное обновление PUT и JSON message/details.
- [x] Расширить OpenAPI до API 1.1.0, сохранив пути и коды; проверить спецификацию и обновить требования. Уточнение U+0000 и расширение ответов явно отмечены в ADR-007.

## 2. Хранение и модель

- [x] Описать проект БД v1 на основе задания и OpenAPI в docs/database/README.md; отделить поля контракта от предложений, зафиксировать ограничения и критерии будущих проверок.

- [x] Выбрать PostgreSQL для постоянного хранения; ADR-006 принят пользователем.
- [x] Выбрать PostgreSQL 18.6, pgx/v5 v5.12.0 и golang-migrate v4.20.1; обоснование и источники — ADR-008.
- [x] Добавить up/down-миграцию таблицы threats и проверить её на реальной PostgreSQL.
- [x] Добавить Compose с healthcheck, постоянным томом, отдельным мигратором и командами Makefile.
- [x] Проверить повторный up, ограничения, сохранность данных при пересоздании контейнера и down/up через make test-db.
- [x] Реализовать domain.MD5/ThreatData/Threat, ParseMD5 и Validate; unit tests допустимых/некорректных MD5 и U+0000, пустых и Unicode-строк.
- [x] Определить минимальный контракт хранилища для трёх сценариев у потребителя: docs/backend/REPOSITORY.md, ADR-009.
- [x] Реализовать согласованные domain-типы, ошибки и Go-интерфейс ThreatRepository в internal/usecase.
- [x] Реализовать Go-адаптер хранения на pgx; проверить Get/Create/Update, конкурентные записи, отсутствие upsert, NULL, даты, ошибки и context на реальной PostgreSQL через make test-repository.

## 3. Сценарии

- [x] Определить спецификацию сервисного слоя: docs/backend/SERVICE.md, ADR-011; операции, валидация, ошибки, context и критерии unit tests.
- [x] Реализовать ThreatService с конструктором от ThreatRepository и unit tests без БД по SERVICE.md.

- [x] Получение: нормализация, найденная запись, отсутствие, техническая ошибка — unit tests.
- [x] Создание: сохранение, некорректные данные и дубликат — unit tests.
- [x] Обновление: замена полей, nil/пустое описание, отсутствие записи и ошибка — unit tests.

## 4. HTTP и запуск

- [x] Спроектировать HTTP layer в docs/backend/HTTP.md (ADR-012): package/API, маршруты, DTO, decoder, ошибки, context, logging/recovery и критерии тестов.
- [x] Реализовать DTO/ограниченный JSON decoder/writer и httptest для отсутствующих/null/неверных полей, unknown fields, Content-Type и лимита тела по HTTP.md.
- [x] При реализации дополнить OpenAPI пояснениями HTTP-политики маршрутизации и разбора запросов, сохранив три операции и их коды.

- [x] Реализовать только три заданных маршрута, DTO и единые ошибки по OpenAPI; httptest для статусов и JSON.
- [x] Добавить серверную конфигурацию через окружение и проверки её корректности.
- [x] Собрать зависимости конструкторами в cmd/api; реализовать запуск и корректное завершение HTTP-сервера.
- [x] Обновить Dockerfile для internal и go.mod/go.sum, проверить Docker-сборку.
- [x] Добавить API в существующий Compose после реализации сервера. PostgreSQL и мигратор уже добавлены.

## 5. Приёмка реализации

- [x] Добавить изолированные integration tests API с выбранным реальным хранилищем; отдельная воспроизводимая команда.
- [x] Проверить 200/201/400/404/409/500, ошибочный JSON и MD5, дубликаты и семантику PUT по контракту.
- [x] Проверить отсутствие утечек внутренних ошибок и чувствительных данных в ответах/логах.
- [x] Обновить README с реальным запуском сервиса и примерами API, выполнить fmt/vet/test/build и Docker smoke check.
- [ ] Сверить результат с исходным заданием и закрыть только проверенные задачи.

## Проверки Bootstrap

Проверено 2026-11-05:

| Проверка | Результат |
| --- | --- |
| Go toolchain | Go 1.27.3 darwin/arm64 скачана через GOTOOLCHAIN; глобальная Go 1.25.6 не изменена |
| go.mod | go mod tidy без изменений; go list -m all выводит только kaspersky-go-test; go.sum не нужен |
| go fmt ./... | Успех, форматирование применено |
| go vet ./... | Успех |
| go test ./... | Успех; cmd/api: [no test files] |
| make build | Успех, bin/api (игнорируется Git) |
| make run | Успех, JSON INFO, выход 0 |
| Smoke бинарника | Default/DEBUG: JSON INFO, выход 0; WARN: тишина, выход 0; invalid/пустое значение: JSON ERROR, выход 1 |
| make docker-build | Успех, kaspersky-go-test:local |
| make docker-run | Успех, JSON INFO, выход 0 |
| Smoke Docker | DEBUG: выход 0; invalid: выход 1; JSON корректен; runtime user 65532:65532 |
| Структура и документы | Нет пустых пакетов и внешних сервисов; локальные Markdown-ссылки проверены; поведение и предложения из пересказа разделены |

Unit/integration tests предметного поведения пока неприменимы: реализация
отсутствует. Smoke-проверки конфигурации выполнены временным скриптом,
фиктивные тестовые файлы не добавлялись. Оригинал и OpenAPI не предоставлены,
поэтому сверка выполнена с предоставленным пересказом; полная сверка
остаётся первой задачей этапа 1.


## Документация БД v1 — 2026-11-05

Создан docs/database/README.md: предложена одна таблица threats для PostgreSQL,
описаны ключ, типы, NULL/default, индексы, конкурентные операции и расширение
модели. Добавлен ADR-006 со статусом «предложено», обновлены архитектура и
актуальные сведения об OpenAPI. Исходный контракт не изменён; миграции,
сервисы и Go-зависимости не добавлены. Выбор хранения и реализация остаются
незавершёнными задачами.

Проверки: сопоставление с OpenAPI и пересказом, проверка локальных Markdown-ссылок
и отсутствия изменений OpenAPI. SQL приведён как проект и на сервере БД
не выполнялся; проверка миграций и интеграционные тесты ещё неприменимы.


## Принятие модели и контракт API 1.1.0 — 2026-11-05

Пользователь принял проект БД v1. ADR-006 переведён в «принято», добавлен
ADR-007 с правилами API. Обновлены OpenAPI, REQUIREMENTS, ARCHITECTURE,
описание БД, AGENTS и README. Исходная спецификация сохранена побайтово
в docs/backend/openapi.original.yaml; текущая — docs/backend/openapi.yaml.
Добавлены description и серверные даты; формализованы lowercase MD5,
полная замена PUT, обязательные поля ответа/ошибок и обработка строк.
Реализация приложения, БД и миграций остаётся не начатой.

Проверки:

- Обе спецификации валидны по openapi-spec-validator; локальные $ref разрешаются.
- Исходный файл совпадает с вложением; пути, методы, operationId и HTTP-коды сохранены.
- Выполнены 57 проверок схем через openapi-schema-validator: примеры,
  обязательные поля, null, U+0000, неверные типы/MD5/даты, пустые строки,
  необязательное description и дополнительные поля.
- Проверены локальные Markdown-ссылки.
- make check: fmt/vet/test прошли; тестовых файлов приложения пока нет.

Валидаторы использованы в отдельном временном Python-окружении;
Go-зависимости и зависимости проекта не добавлены. Проверки схем не
проверяют работу будущего сервера, семантику игнорирования полей или SQL.


## PostgreSQL и миграции — 2026-11-05

По запросу пользователя реализован инфраструктурный этап:

- PostgreSQL 18.6-bookworm и golang-migrate v4.20.1 закреплены digest в Compose.
- pgx/v5 v5.12.0 выбран для будущего адаптера; версия и минимальная Go 1.25.0
  подтверждены go list -m -json, с Go 1.27.3 совместим. Пока не добавлен в go.mod.
- Добавлены migrations/000001_create_threats.up.sql и .down.sql.
- Compose содержит БД, healthcheck, loopback-порт, постоянный том
  /var/lib/postgresql и одноразовый мигратор с read-only SQL.
- Добавлены db-up/db-down/db-shell, migrate-up/down/version и test-db в Makefile.
- Обновлены env example, README, требования, архитектура, описание БД и AGENTS;
  принят ADR-008. API/OpenAPI и Go-бизнес-код не изменены.

Фактические проверки:

| Проверка | Результат |
| --- | --- |
| Docker images | Оба образа скачаны, версии и digest доступны на arm64 |
| Compose config | config --quiet прошёл в отдельном тестовом проекте |
| make test-db | Успех на PostgreSQL 18.6; отдельные проект, том и случайный порт |
| Миграции | up, повторный up, down 1 и повторный up прошли; версия 1, dirty=false |
| Ограничения | PK/NOT NULL/CHECK отклоняют дубликаты, NULL и некорректные MD5, включая uppercase и хвостовые пробел/перевод строки |
| Поля/default | Одинаковые имена и пустое имя разрешены, NULL/пустое description различаются, даты заполнены и равны при INSERT |
| Постоянный том | Строка и версия миграции сохранились после down/up с пересозданием контейнера |
| Изоляция | Тестовый проект и его том удалены; другие Compose-проекты не затронуты |
| make check / make build | fmt/vet/test и сборка прошли; Go-тестовых файлов пока нет |
| Документация/Makefile | Локальные ссылки и whitespace проверены, команды Make проверены через dry-run |

Это проверки SQL и инфраструктуры. HTTP, Go-адаптер, нормализация входного
MD5 и поведение PUT остаются не реализованы и не отмечены выполненными.


## Контракт repository layer — 2026-11-05

Создан docs/backend/REPOSITORY.md: GetByMD5/Create/Update, планируемые
сигнатуры и типы, предусловия, возвращаемые данные, ошибки, SQL NULL,
серверные даты, конкурентные операции, context, владение пулом и критерии
будущих integration tests адаптера. Добавлен ADR-009, ссылки в архитектуре,
AGENTS и README. Контракт определён; Go-интерфейс, доменные типы, адаптер
и его тесты остаются незавершёнными задачами.

Проверки: сопоставление со схемой threats, API 1.1.0 и принятыми решениями;
локальные Markdown-ссылки и whitespace корректны. make check прошёл
(fmt/vet/test; Go-тестовых файлов пока нет). SQL, Compose, OpenAPI,
Go-код и зависимости в этой задаче не изменены. make test-db повторно
не запускался: миграции/инфраструктура не менялись; критерии будущих
тестов репозитория не выдаются за выполненные проверки.


## Реализация repository layer — 2026-11-05

Реализованы internal/domain (модель/валидация/ошибки), интерфейс у usecase
и pgx-адаптер в internal/repository/postgres. Каждый метод выполняет один
параметризованный запрос; Create/Update возвращают строку через RETURNING.
Пул принадлежит вызывающему коду. NULL и пустая строка различаются,
даты возвращаются в UTC. Ожидаемые ошибки отделены от технических;
context и причины технических ошибок доступны через errors.Is/As.

Добавлены pgx/v5 v5.12.0 и go.sum; ORM, SQL mocks и новые бизнес-функции
не добавлены. Dockerfile копирует internal и загружает зависимости.
Добавлен make test-repository: runner создаёт изолированную PostgreSQL,
применяет существующую миграцию, запускает 11 Go integration tests с -race,
после чего удаляет только свой тестовый проект и том.

Проверки:

| Команда/проверка | Результат |
| --- | --- |
| make check | fmt/vet/test прошли; domain unit tests выполняются без БД |
| make test-repository | Все 11 integration tests прошли на PostgreSQL 18.6 с -race |
| Конкурентные операции | Один Create успешен, второй — ErrThreatAlreadyExists; пара Update возвращает целостные строки, итог соответствует одной из записей |
| Ошибки | Not found/duplicate, другой UNIQUE, CHECK, закрытый пул, canceled/expired context, отмена во время SQL и deadline при ожидании пула проверены |
| Поля | SQL NULL/пустая строка/Unicode, отсутствие trim, параметризация, неизменный created_at и присваивание updated_at проверены |
| make test-db | Регрессия SQL/Compose, повторный up, persistence и rollback/reapply прошли |
| go vet -tags=integration ./... | Успех |
| go mod verify | Все модули проверены |
| make build / make docker-build | Успех |

Первоначальный тест ошибочно ожидал гарантированный rollback при отмене
заблокированного UPDATE. Наблюдение показало документированную неопределённость
результата записи после context error; тест исправлен без усиления гарантий
адаптера. Повторный полный запуск integration tests успешен.

Архитектура, REPOSITORY, AGENTS, README и ADR-010 синхронизированы.
OpenAPI, SQL-миграции и Compose не изменены. API и usecase-сценарии,
конфигурация/жизненный цикл пула в cmd/api остаются не реализованы.

## Спецификация сервисного слоя — 2026-11-05

Создан docs/backend/SERVICE.md и ADR-011: ThreatService в internal/usecase,
три операции, constructor DI, обязательные ParseMD5/Validate до repository,
полная замена полей при Update, ошибки, context и критерии будущих тестов.
Уточнено разделение транспортной и доменной валидации; дополнительные
service/usecase-слои, DTO, предварительные SELECT, retry и транзакции не нужны.
Обновлены архитектура, AGENTS и README.

Проверки: спецификация сопоставлена с OpenAPI, существующими domain-типами
и интерфейсом ThreatRepository. Локальные Markdown-ссылки и whitespace
проверены. make check (fmt/vet/test) прошёл. Go-код, OpenAPI, миграции,
Compose и зависимости не изменялись; тесты PostgreSQL повторно не запускались.
Сервис и его тесты ещё не реализованы; завершено только определение контракта.


## Реализация ThreatService — 2026-11-05

Добавлен internal/usecase/threat_service.go: конструктор от ThreatRepository,
GetByMD5/Create/Update, ctx.Err на входе, ParseMD5 и Validate перед хранилищем,
ровно один repository-вызов для валидного запроса. Результат хранилища
возвращается без изменения полей/дат; при ошибке — нулевая Threat.
Ошибки передаются без повторной обёртки и сохраняют errors.Is/As.
Поздний context.Canceled не подменяет полученный успех или техническую причину.

Добавлены ручной fake и unit tests на нормализацию, валидацию, порядок
проверок, передачу аргументов/context, NULL/пустые строки, ошибки и отсутствие
повторных/предварительных обращений. Сквозной TestThreatServiceWithPostgres
проверяет Create uppercase → Get → Update с очисткой описания → дубликат,
отсутствие записи и доменные ошибки на реальном адаптере.

Фактические проверки:

| Проверка | Результат |
| --- | --- |
| make check | fmt/vet/test прошли |
| go test -race ./... | Все unit tests прошли, гонок не обнаружено |
| go test -cover ./internal/usecase | 100% statements сервисного пакета покрыты unit tests |
| make test-repository | 12 integration tests repository/service прошли на PostgreSQL 18.6 с -race; тестовый проект/том удалены |
| go vet -tags=integration ./... | Успех |
| make build | Успех |
| Документация | Обновлены SERVICE, REPOSITORY, ARCHITECTURE, REQUIREMENTS, ADR-011, AGENTS и README; локальные ссылки/whitespace проверены |

Новых зависимостей нет. HTTP, cmd/api, конфигурация, repository, OpenAPI,
SQL-миграции и Compose не изменены. Docker и migration smoke tests отдельно
не повторялись: их конфигурация и SQL не менялись; новая цепочка проверена
существующим изолированным runner на той же миграции.

## Проект HTTP layer — 2026-11-05

Создан docs/backend/HTTP.md: один пакет httptransport, consumer interface
ThreatService и NewHandler, path-only ServeMux с явными методами, DTO,
ограниченный JSON decoder, отсутствие/null/строки и unknown fields,
централизованный JSON/error writer, context, recovery и slog.
Проектные HTTP-решения (1 MiB, Content-Type, 400 для неверного тела/media,
404/405 маршрутизации) отделены от требований исходного задания.
Описаны критерии httptest и будущих HTTP → service → PostgreSQL tests.
Добавлен ADR-012, обновлены архитектура, AGENTS, README и ограничения этапа.

Проверки: сопоставление с API 1.1.0, сигнатурами сервиса и его правилами
валидации; технические особенности ServeMux/MaxBytesReader/encoding-json
и ParseMediaType сверены с документацией stdlib. Локальные Markdown-ссылки
и whitespace корректны; make check (fmt/vet/test) прошёл.
HTTP-код, OpenAPI, сервис, repository, SQL, Compose и зависимости не менялись.
Интеграционные тесты БД повторно не запускались: изменена только документация.
Готовность HTTP-реализации и её тестов не заявляется.


## Реализация HTTP layer — 2026-11-05

Реализован internal/transport/http (httptransport): NewHandler от локального
интерфейса сервиса, ServeMux и явные методы, DTO, ограниченный JSON decoder,
единый writer/error mapping, response state, recovery и slog logging.
Доменную проверку вызывает существующий сервис; прямой связи HTTP с pgx нет.
Проверяются точные имена/присутствие/типы/null до преобразования DTO;
unknown fields игнорируются, nil/пустое описание различаются.

В OpenAPI info.description добавлены пояснения политики media type/размера,
405/Allow, HEAD и канонизации URL. Схемы, пути и коды трёх операций сохранены.
Добавлен HTTP → service → repository → PostgreSQL test, проверяющий
200/201/400/404/409/500, uppercase duplicate, очистку описания и отсутствие upsert.
make test-integration запускает весь suite; make test-repository сохранён как алиас.

Фактические проверки:

| Проверка | Результат |
| --- | --- |
| make check | fmt/vet/test прошли |
| go test -race ./... | Все unit tests прошли, гонок нет |
| make test-integration | 13 Go integration tests прошли на PostgreSQL 18.6 с -race; собственный проект/том удалены |
| HTTP protocol tests | HEAD без тела/вызова сервиса, размер chunked body, panic до/после начала ответа, ошибка/short write и отсутствие второго JSON проверены |
| HTTP data/error tests | DTO/UTC/NULL, required/type/null, unknown/duplicate/case-sensitive fields, Content-Type, context, безопасные ошибки и однократный Error log проверены |
| go vet -tags=integration ./... | Успех |
| make build | Успех |
| OpenAPI | Валидна по openapi-spec-validator; сравнение с git подтверждает изменение только info.description |
| Документация | Локальные Markdown-ссылки и whitespace проверены |

Обновлены HTTP, ARCHITECTURE, REQUIREMENTS, AGENTS, ADR-012 и README.
Новых Go-зависимостей нет. Сервис, repository, миграции, Compose и cmd/api
не изменены. Handler доступен для подключения, но production сервер пока
не запускается; конфигурация, lifecycle/wiring и API в Compose не завершены.
Текущие integration tests используют httptest.Server, не cmd/api.


## Runtime, сервер и Compose — 2026-11-05

Реализован утверждённый пользователем план. internal/config.Load читает
окружение и проверяет defaults/пустые значения/диапазоны/duration/SSL/адреса,
формирует безопасный URL. cmd/api создаёт пул, проверяет Ping, миграцию 1,
dirty=false и доступность threats до открытия listener; DDL не выполняет.
Собраны repository → service → HTTP и настоящий сервер с timeouts.

На SIGINT/SIGTERM выполняется drain на отдельном context; request BaseContext
не отменяется сигналом до истечения shutdown timeout. При timeout — отмена
запросов и Close, затем закрытие пула. Exit 0 штатный, ошибки startup/shutdown
дают 1. Fatal JSON stderr не подавляется LOG_LEVEL, в том числе ERROR+1;
ErrorLog net/http направлен в slog. Пароль/DSN не выводятся.

Добавлены API в Compose, stop_grace_period=30s, CA bundle в scratch,
make up/down/logs, docker-run → up и make test-runtime. make up выполняет
проверенный порядок db-up → migrate-up → api up --build; down сохраняет том.
Обновлены .env.example, README с curl-примерами, AGENTS, архитектура,
требования, контракты и ADR-013/RUNTIME.md. Python cache исключён из Git.

Фактические проверки итоговой версии:

| Проверка | Результат |
| --- | --- |
| make check | fmt/vet/unit tests прошли |
| go test -race ./... | Config, lifecycle, domain, service и HTTP unit tests прошли, гонок нет |
| make test-integration | 13 существующих Go integration tests прошли на PostgreSQL 18.6 с -race |
| make test-runtime: startup failures | Отсутствие/dirty/другая версия схемы, недоступная таблица, ошибка config, соединения и занятый порт дают exit 1, один Error log, без утечки пароля и открытия HTTP |
| make test-runtime: process | Настоящий бинарник POST/GET/PUT; SIGTERM/SIGINT дают exit 0; порт освобождён |
| Lifecycle unit tests | Listener освобождается, in-flight запрос заканчивается после сигнала, по timeout context отменён и соединение закрыто |
| make test-runtime: Compose | make up/down, Docker build/HTTP, сохранность данных после пересоздания контейнеров, stop API с exit 0 прошли |
| make test-db | Up/повторный up, constraints/default, persistence и down/up прошли |
| make build / make docker-build | Успех; текущий bin/api и локальный образ собраны |
| go vet -tags=integration ./... / go mod verify | Успех |
| Документация | Локальные ссылки/whitespace и git diff --check прошли |

Тесты использовали отдельные случайные Compose-проекты, порты и тома;
контейнеры/сети/тома удалены в finally, обычная БД пользователя не затронута.
Новых Go-зависимостей, SQL-миграций, endpoints и изменений бизнес-контракта нет.
Оригинальный документ Kaspersky всё ещё отсутствует: его сверка остаётся
открытой задачей и не отмечена завершённой.


## Уточнение имени проекта — 2026-11-05

- [x] Использовать kaspersky-go-test в go.mod, импортах кода/тестов,
  имени Docker-образа Makefile и документации; ADR-002 актуализирован.
- [x] Проверить fmt/vet/unit tests через make check и сборку make build.
- [x] Проверить go vet -tags=integration ./... и компиляцию integration tests
  через go test -tags=integration -run '^$' ./tests/integration.
- [x] Подтвердить имя модуля собранного bin/api через go version -m.

Проверки прошли. БД/Compose smoke tests повторно не запускались: изменены
имена модуля/импорты/документация, поведение API и схема БД не менялись.
