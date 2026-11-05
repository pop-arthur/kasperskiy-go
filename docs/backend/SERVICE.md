# Сервисный слой: спецификация v1

Дата: 2026-11-05. Статус: реализовано и проверено (ADR-011).
Код — [threat_service.go](../../internal/usecase/threat_service.go),
unit tests — [threat_service_test.go](../../internal/usecase/threat_service_test.go),
сквозной тест — [service_test.go](../../tests/integration/service_test.go).

Основание: [требования](../REQUIREMENTS.md), [OpenAPI 1.1.0](openapi.yaml),
[контракт repository](REPOSITORY.md), существующие
[доменные типы](../../internal/domain/threat.go),
[ParseMD5](../../internal/domain/md5.go) и
[ThreatRepository](../../internal/usecase/threat_repository.go).

## Назначение и место в архитектуре

Сервисный слой в этом проекте — это usecase-слой, а не дополнительный
слой между usecase и repository. Его реализация находится в internal/usecase.
Он выполняет три сценария: получить, создать, обновить угрозу.

Поток: HTTP transport → ThreatService → ThreatRepository → PostgreSQL.
Сервис зависит от domain и собственного интерфейса ThreatRepository;
не импортирует HTTP, pgx, SQL, конкретный repository или окружение.

Главная ответственность сервиса — обязательно вызвать доменную валидацию
и нормализацию до обращения к repository, затем передать результат/ошибку
вызывающему коду. ThreatService вызывает существующие проверки domain
во всех трёх операциях; невалидные данные не передаются в repository.

## Структура и Go-контракт

Реализация — internal/usecase/threat_service.go; unit tests — рядом.
ThreatRepository уже определён в этом пакете; его сигнатуры не меняются.
Ниже сигнатуры реализованного сервиса:

```go
// package usecase
// context — стандартный пакет; domain — kaspersky-go-test/internal/domain.
type ThreatService struct {
    repository ThreatRepository
}

func NewThreatService(repository ThreatRepository) *ThreatService

func (service *ThreatService) GetByMD5(ctx context.Context, md5 string) (domain.Threat, error)
func (service *ThreatService) Create(ctx context.Context, md5 string, data domain.ThreatData) (domain.Threat, error)
func (service *ThreatService) Update(ctx context.Context, md5 string, data domain.ThreatData) (domain.Threat, error)
```

Конструктор требует ненулевую зависимость, сохраняет её без I/O, проверки
соединения или запуска миграций. Сервис не владеет пулом и не закрывает его.
Создаёт зависимости и управляет их жизненным циклом composition root.

MD5 на входе — сырая строка: именно сервис гарантирует её разбор через
ParseMD5. Принимать сразу domain.MD5 означало бы переложить эту гарантию
на transport и позволить unchecked cast обойти нормализацию.
ThreatData уже описывает изменяемые поля, отдельные сервисные DTO/command
для совпадающего набора полей не нужны. HTTP DTO остаются в transport.

Дополнительный интерфейс ThreatService заранее не вводить: будущий
transport при необходимости определит минимальный интерфейс у потребителя.
Не создавать internal/service, базовый Service или DI-контейнер.

## Общий порядок выполнения

1. Проверить ctx.Err() на входе. Уже отменённый/истёкший context возвращает
   соответствующую ошибку, repository не вызывается. При одновременной
   ошибке входных данных и отменённом context приоритет имеет context.
2. Вызвать domain.ParseMD5 для сырой строки. При ошибке вернуть её,
   repository не вызывается.
3. Для Create/Update вызвать data.Validate(). При ошибке вернуть её,
   repository не вызывается. Если неверны и MD5, и поля, вернуть ошибку MD5;
   агрегировать несколько ошибок в v1 не требуется.
4. Вызвать ровно один соответствующий метод repository с тем же ctx,
   каноническим MD5 и неизменённым ThreatData.
5. Вернуть сохранённую domain.Threat или ошибку. При ненулевой ошибке
   возвращать нулевую Threat; её содержимое не является результатом операции.

После вызова repository не подменять результат поздним ctx.Err(): если
repository вернул успешную запись, сервис возвращает этот успех. Ошибки
repository не классифицировать повторно по состоянию context или тексту.
Отмена между входной проверкой и запросом передаётся через исходный ctx;
repository может быть вызван с уже отменившимся context.

## Сценарии

| Сценарий | Подготовка | Единственный вызов repository | Результат |
| --- | --- | --- | --- |
| GetByMD5 | ParseMD5 | GetByMD5(ctx, canonicalMD5) | Найденная Threat или ErrThreatNotFound |
| Create | ParseMD5, ThreatData.Validate | Create(ctx, canonicalMD5, data) | Созданная Threat или ErrThreatAlreadyExists |
| Update | ParseMD5, ThreatData.Validate | Update(ctx, canonicalMD5, data) | Обновлённая Threat или ErrThreatNotFound |

### Получение

Сервис не изменяет данные, не создаёт запись при отсутствии и не
подставляет пустую угрозу вместо not found. Верхний/смешанный регистр
MD5 адресует ту же запись, что и нижний.

### Создание

Сервис не делает предварительный Get/Exists: дубликат определяет repository
через уникальность БД. Дубликат не превращается в обновление. Даты не
принимаются от клиента и не вычисляются через time.Now().

### Обновление

Сервис передаёт только новый name/description. Предварительного чтения,
слияния со старой записью или создания отсутствующей записи нет.
Description == nil должен дойти до repository как nil и очистить описание;
указатель на пустую строку сохраняет пустую строку. Не менять MD5,
не передавать клиентские даты и не добавлять version/ETag.

Семантика конкурентных записей остаётся в контракте repository: Create
защищён уникальностью БД; Update использует последнюю успешную запись.
Локальный mutex в сервисе не заменяет эти гарантии и не требуется.

## Валидация и границы слоёв

| Проверка | Ответственный слой | Правило |
| --- | --- | --- |
| Корректность JSON, один объект, типы и обязательные поля | Transport | До вызова сервиса; JSON null для известных строк запрещён |
| Неизвестные JSON-поля, клиентские даты и md5 в теле PUT | Transport | Игнорировать; MD5 для PUT брать только из пути |
| Формат/нормализация MD5 | Domain, вызывается сервисом | ParseMD5: ровно 32 ASCII hex, lowercase, без trim |
| U+0000 в name/description | Domain, вызывается сервисом | ThreatData.Validate; пустые строки/пробелы разрешены, без trim |
| Уникальность и существование записи | Repository/БД | Не дублировать предварительными SELECT в сервисе |
| HTTP-статус, JSON message/details, формат дат | Transport | По OpenAPI; технические подробности не выдавать клиенту |

ThreatData.Name — string: на этой границе нельзя отличить отсутствовавшее
поле от пустого имени. Это допустимо, поскольку пустое имя разрешено,
а присутствие поля обязан проверить transport. Description == nil означает
отсутствие описания; transport должен отклонить явный JSON null до
преобразования DTO, иначе информация о нарушении контракта потеряется.

Сервис не вводит новые minLength/maxLength, enum, defaults, обрезку строк,
автоматическое имя угрозы, дополнительные поля или бизнес-правила.
Результат repository возвращается без пересоздания дат или JSON-преобразований.

## Ошибки и context

Все ошибки остаются обычными Go error. Реализация передаёт исходную
ошибку без повторной обёртки: repository уже добавляет контекст операции,
а domain возвращает sentinel ошибки. errors.Is/As сохраняются. Не сравнивать
error.Error() и не заменять все ошибки на общее «не удалось выполнить».

| Ошибка сервиса | Источник | Будущее соответствие HTTP |
| --- | --- | --- |
| domain.ErrInvalidMD5 | ParseMD5 | 400 |
| domain.ErrInvalidThreatData | ThreatData.Validate | 400 |
| domain.ErrThreatNotFound | Repository, Get/Update | 404 |
| domain.ErrThreatAlreadyExists | Repository, Create | 409 |
| context.Canceled / context.DeadlineExceeded | Входной context или repository | Решает transport; новых кодов эта спецификация не вводит |
| Иная ошибка | Repository | 500, без раскрытия причины клиенту |

Технические ошибки не превращать в not found, duplicate или invalid input.
Не добавлять новый ErrService, который скрывает исходную причину.
Сервис не создаёт context.Background, не сохраняет ctx в структуре и не
устанавливает скрытый timeout. Deadline задаёт вызывающий слой.

Автоматических retry нет: ошибка после отправки INSERT/UPDATE не гарантирует,
что запись не произошла. Дополнительная транзакция сервиса для одного
repository-вызова не нужна. Логи ошибок — один раз на внешней границе,
без дублирующего Error-логирования в сервисе и repository.

Сервис не хранит состояние запросов и должен допускать одновременные вызовы
при потокобезопасном repository. Вызывающий код не должен изменять переданный
Description параллельно с выполнением операции; сервис не удерживает
данные после возврата.

## Проверяемый результат реализации

Unit tests в internal/usecase выполняются без PostgreSQL. Использовать
небольшую ручную тестовую реализацию ThreatRepository, которая записывает
вызовы/аргументы и возвращает заданный результат или error. Новая mocking
зависимость не нужна. Невыбранные методы fake должны приводить к провалу
теста, чтобы обнаружить скрытый предварительный Get/Exists.

| Проверка | Ожидаемый результат |
| --- | --- |
| Valid lowercase MD5 в каждом сценарии | Ровно один вызов соответствующего метода repository |
| Uppercase/mixed MD5 в каждом сценарии | Repository получает lowercase |
| Неверный MD5 в каждом сценарии | ErrInvalidMD5; ни одного обращения к repository |
| Create/Update с U+0000 в name или description | ErrInvalidThreatData; repository не вызван |
| Пустые строки, пробелы, Unicode | Переданы без trim/подмены |
| Create без description | В repository передан nil |
| Update с nil/пустым description | Передан nil/указатель на пустую строку; предварительного чтения нет |
| Успешный результат repository | Все поля и даты возвращены без изменений |
| Not found, duplicate, техническая ошибка | Исходная причина доступна через errors.Is/As; нулевая Threat при ошибке |
| Уже canceled/expired context | Соответствующая ошибка без repository-вызова |
| Context с deadline/значениями | Передан тот же context, без Background или скрытого timeout |
| Repository возвращает context error | Сохранена причина, повторных вызовов нет |
| Context отменился, но repository вернул успех | Полученный успех не заменён поздней ошибкой context |

Сквозная проверка service + реальный repository реализована и выполнена:
создание по uppercase MD5 → получение по lowercase → обновление с nil
описанием → дубликат и отсутствующая запись. Проверяются также ошибки
невалидных MD5 и полей. Команда make test-repository выполняет этот тест
и тесты адаптера на PostgreSQL с -race. Это не заменяет будущие HTTP-тесты JSON/кодов.

## Текущее состояние и следующий этап

Три сценария и их unit/integration tests реализованы без новых зависимостей.
Unit tests выполняются обычным go test ./..., в том числе с -race.
HTTP transport, подключение/жизненный цикл пула и сборка сервера реализованы
следующими этапами; детали — [RUNTIME.md](RUNTIME.md). Контракт сервиса,
repository и SQL-миграции при runtime wiring не изменены.
