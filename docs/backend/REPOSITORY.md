# Repository layer: контракт v1

Дата: 2026-11-05. Статус: контракт реализован (ADR-010).
Доменные типы — internal/domain, интерфейс — internal/usecase/threat_repository.go,
адаптер — internal/repository/postgres/threat.go. Сервисные сценарии реализованы
в internal/usecase/threat_service.go; HTTP API пока нет.

Основание: [требования](../REQUIREMENTS.md), [OpenAPI 1.1.0](openapi.yaml),
[схема БД](../database/README.md),
[миграция threats](../../migrations/000001_create_threats.up.sql), ADR-006–009.

## Необходимые операции

Repository отвечает за чтение и запись threats и преобразование ошибок
хранилища в понятные внутренним слоям результаты. Достаточно трёх методов:

| Метод | Вход | Успех | Ожидаемая ошибка |
| --- | --- | --- | --- |
| GetByMD5 | context, канонический MD5 | Полная сохранённая угроза | ErrThreatNotFound |
| Create | context, канонический MD5, name/description | Созданная угроза с датами БД | ErrThreatAlreadyExists |
| Update | context, канонический MD5, новые name/description | Обновлённая угроза с датами БД | ErrThreatNotFound |

Все методы также могут вернуть техническую ошибку или ошибку context.
При ненулевой ошибке возвращаемое значение угрозы не использовать.
Отсутствие записи — ошибка, а не нулевая структура с nil error.

## Go-контракт

Интерфейс определён у потребителя, в internal/usecase; конкретный адаптер
расположен в internal/repository/postgres. Domain и usecase не импортируют
pgx, SQL, HTTP или конкретную реализацию repository.
Ниже сигнатуры реализованного интерфейса:

```go
// package usecase
// context — стандартный пакет; domain — kaspersky-go-test/internal/domain.
type ThreatRepository interface {
    GetByMD5(ctx context.Context, md5 domain.MD5) (domain.Threat, error)
    Create(ctx context.Context, md5 domain.MD5, data domain.ThreatData) (domain.Threat, error)
    Update(ctx context.Context, md5 domain.MD5, data domain.ThreatData) (domain.Threat, error)
}
```

Реализованные доменные данные:

| Тип | Содержимое |
| --- | --- |
| domain.MD5 | Именованный строковый тип; валидный MD5 в lowercase после доменной проверки/нормализации |
| domain.ThreatData | Name string и Description *string — только изменяемые поля |
| domain.Threat | MD5 domain.MD5, Name string, Description *string, CreatedAt/UpdatedAt time.Time |

ThreatData общий для Create/Update: набор изменяемых полей у них совпадает.
MD5 передаётся отдельно; даты не принимаются, поэтому вызывающий код не
может через контракт repository назначить created_at/updated_at или изменить ключ.
HTTP DTO не передавать в repository. Именованный MD5 сам по себе не заменяет
валидацию: создавать и нормализовать его должен доменный код до обращения
к repository, а не простой unchecked cast в обработчике.

Description == nil означает SQL NULL; указатель на пустую строку — SQL ''.
При чтении сохранять это различие. Не заменять nil на пустую строку.
JSON null и отсутствие description различает HTTP-слой: null отклоняет,
отсутствие переводит в nil. Repository не занимается JSON-сериализацией.

## GetByMD5

- Выполнять поиск по точному каноническому md5, без изменения данных.
- Возвращать все пять полей таблицы, включая description и даты.
- Если строки нет, возвращать domain.ErrThreatNotFound.
- Не создавать запись, не обновлять updated_at, не выполнять поиск по name.

## Create

- Вставлять только md5, name и description. Даты выставляет DEFAULT БД.
- Возвращать фактически вставленную строку через INSERT ... RETURNING,
  без последующего SELECT и без локального time.Now().
- Повторный MD5 не меняет существующую запись, её описание или даты.
- Уникальность обеспечивает threats_pkey. Предварительный SELECT/Exists
  не нужен и не является гарантией от гонки.
- При двух конкурентных Create одного ключа, если нет иных сбоев, один
  создаёт запись, другой получает ErrThreatAlreadyExists.
- Никакого upsert или ON CONFLICT DO UPDATE.

## Update

- Один UPDATE по md5 заменяет name и description; nil сбрасывает описание
  в SQL NULL. Это полная замена изменяемых полей, не PATCH.
- Не менять md5/created_at. Явно задавать updated_at = CURRENT_TIMESTAMP
  в том же операторе, даже если значения полей совпали с прежними.
- Возвращать строку через UPDATE ... RETURNING. Отсутствие возвращённой
  строки означает ErrThreatNotFound; не создавать запись.
- Конкурентные обновления используют уже принятую семантику последней
  успешной записи; version/ETag/optimistic locking не вводятся.
- Полученная через RETURNING запись — результат этой операции; следующий
  конкурентный PUT может изменить её после завершения запроса.

Даты должны возвращаться как time.Time в UTC; их JSON-формат — обязанность
transport. CURRENT_TIMESTAMP имеет семантику времени начала транзакции,
не номера версии; updated_at не обязан строго возрастать.

## Ошибки

Два sentinel error находятся в domain; сравнивать через errors.Is.
Для валидации дополнительно есть ErrInvalidMD5 и ErrInvalidThreatData.
ParseMD5 и ThreatData.Validate вызываются до обращения к адаптеру.
Контракт ошибок хранения:

| Ситуация в адаптере | Результат repository | Последующее соответствие API |
| --- | --- | --- |
| pgx.ErrNoRows при GetByMD5/Update | domain.ErrThreatNotFound | 404 |
| SQLSTATE 23505, ограничение threats_pkey при Create | domain.ErrThreatAlreadyExists | 409 |
| context отменён / deadline истёк | Ошибка с сохранённым errors.Is для context.Canceled/DeadlineExceeded, когда причина отмены установлена | Решает вызывающий слой; новых HTTP-кодов этот документ не вводит |
| Соединение/пул/SQL/Scan или другой неожиданный сбой | Техническая ошибка с контекстом операции и сохранением причины через %w | 500 без технических подробностей |

Адаптер определяет SQLSTATE/ConstraintName через errors.As и pgconn.PgError,
а не по тексту сообщения. Не любой 23505 означает дубликат MD5: проверять
конкретное ограничение. Не превращать нарушение CHECK/NOT NULL в ошибку
дубликата или автоматический HTTP 400: для валидированного входа это
нарушение внутреннего инварианта, требующее диагностики.

Repository не возвращает HTTP-коды, message/details, DTO или pgx-типы как
часть публичных сигнатур. Внутренние слои не должны распознавать ошибки
драйвера. Техническая причина может оставаться в цепочке error для диагностики,
но клиент её не получает. Логировать ошибку один раз на внешней границе;
в repository не добавлять повторное Error-логирование и вывод параметров/DSN.

## Границы ответственности

- Transport проверяет JSON, наличие обязательных полей, недопустимый null,
  игнорирует неизвестные поля и формирует HTTP-ответы.
- Domain/usecase обеспечивают валидацию и lowercase MD5, отсутствие U+0000,
  допустимость данных и передачу уже проверенного ThreatData в repository.
  Пустое name/description и пробелы допустимы, trim не делать.
- Repository выполняет параметризованные запросы и преобразует SQL NULL,
  строки, время и ошибки. Не нормализует MD5 повторно и не добавляет правила,
  которых нет в контракте. БД остаётся последней защитой инвариантов.
- Composition root создаёт pgxpool.Pool, проверяет подключение до готовности
  приложения, передаёт пул конструктору адаптера и закрывает его при остановке.
  Repository не читает окружение, не создаёт и не закрывает переданный пул.
- Миграции выполняются отдельно через golang-migrate, не в конструкторе.

Все обращения к БД получают переданный context, без подмены на Background.
Не хранить context в структуре. Ограничения времени задаёт вызывающий слой
и конфигурация подключения; не вводить скрытый жёсткий timeout внутри методов.

Для каждой операции достаточно одного SQL-оператора; многооперационная
транзакция и внешний transaction manager пока не нужны. SQL задавать с
явными колонками и параметрами $1/$2/$3, без SELECT * и конкатенации входа.
Адаптер должен быть безопасен для одновременных вызовов с общим pgxpool,
без собственного общего изменяемого состояния запросов.

Не делать автоматических повторов записи при неопределённом результате
(например, обрыв соединения): ошибка получения ответа не доказывает, что
INSERT/UPDATE не был выполнен. Не добавлять обещаний exactly-once.

В интерфейсе не нужны Delete, List, Exists, Count, Save/upsert, Begin/Commit,
Ping/Close, фильтры, пагинация, batch, кэш и аудит. Инструменты жизненного
цикла пула не являются операциями repository для бизнес-сценариев.

## Проверяемый результат реализации

Integration tests Go-адаптера вызывают методы с реальной PostgreSQL
и миграцией v1: make test-repository. Отдельный make test-db по-прежнему
проверяет только SQL/Compose. Runner создаёт и удаляет собственную БД/том;
Go-тесты включаются тегом integration и запускаются с -race.

| Проверка | Ожидаемый результат |
| --- | --- |
| Create валидной записи | Возвращены все поля, MD5 совпал, даты БД заполнены и равны |
| GetByMD5 существующей/отсутствующей записи | Сохранённые поля / errors.Is(ErrThreatNotFound) |
| Description nil и указатель на пустую строку | Различаются после Create/Get/Update |
| Одинаковые name у разных MD5, пустые строки/пробелы | Сохраняются без дополнительных ограничений |
| Повторный Create | ErrThreatAlreadyExists; прежняя строка и даты не изменены |
| Два конкурентных Create одного MD5 | Один успех, один ErrThreatAlreadyExists, одна строка |
| Update существующей записи | Поля заменены, md5/created_at сохранены, updated_at назначен БД |
| Update с nil description | Старое описание стало SQL NULL |
| Update без изменения значений | Успех, updated_at присвоен; не требовать строгого увеличения времени |
| Update отсутствующей записи | ErrThreatNotFound; новая строка не появилась |
| Отмена context / deadline | Управляемое завершение, установленная причина отмены доступна через errors.Is |
| Технический сбой | Ненулевая ошибка с сохранённой причиной; не маскируется под not found/duplicate |

Нормализацию uppercase MD5 и отклонение неправильного ввода тестировать
в domain/usecase/transport: repository получает уже канонический ключ.
При тестировании не сравнивать время с локальным time.Now() на точное
равенство и не использовать произвольный sleep ради updated_at.

## Реализация и следующий этап

Типы, валидация, интерфейс, адаптер и integration tests реализованы.
В адаптере три параметризованных запроса и общий scanThreat; конструктор
NewThreatRepository принимает caller-owned *pgxpool.Pool. Ожидаемые ошибки
маппятся на domain sentinel, технические оборачиваются через %w.
Простая передача context в pgx сохраняет причины отмены без подмены
технических ошибок состоянием ctx.Err().

Проверка отмены заблокированного UPDATE подтвердила оговорку контракта:
возврат context.Canceled не гарантирует rollback; сервер может завершить
запись до обнаружения разрыва соединения. Автоповторов нет.

Usecase-сценарии реализованы поверх этого контракта (см. SERVICE.md).
HTTP transport и lifecycle пула в composition root реализованы;
подробности — [RUNTIME.md](RUNTIME.md). OpenAPI, схема таблицы и миграции не изменены.

Справочные источники технических деталей:

- [pgx: QueryRow, ErrNoRows и Scan](https://pkg.go.dev/github.com/jackc/pgx/v5).
- [PostgreSQL SQLSTATE и структурированные ошибки](https://www.postgresql.org/docs/18/errcodes-appendix.html).
- [PostgreSQL RETURNING](https://www.postgresql.org/docs/18/dml-returning.html).
