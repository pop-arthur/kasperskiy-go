# HTTP layer: спецификация v1

Дата: 2026-11-05. Статус: реализовано и проверено (ADR-012).
Код — [internal/transport/http](../../internal/transport/http/handler.go),
unit tests — handler_test.go/middleware_test.go, сквозной тест —
[http_test.go](../../tests/integration/http_test.go).
Основание: [OpenAPI 1.1.0](openapi.yaml), [требования](../REQUIREMENTS.md),
[сервисный контракт](SERVICE.md) и существующий
[ThreatService](../../internal/usecase/threat_service.go).

## Назначение и границы

HTTP-слой переводит запрос в вызов сервиса и результат в JSON-ответ.
Он отвечает за маршрутизацию, форму JSON, обязательность/типы полей,
HTTP-коды и безопасное представление ошибок. Формат MD5, lowercase и U+0000
проверяет сервис через domain; уникальность и хранение остаются repository/БД.

Не обращаться к repository напрямую, не добавлять SQL/pgx в transport,
не дублировать ParseMD5/ThreatData.Validate в обработчиках. Вызов сервиса
получает исходный r.Context() без Background, скрытых retry или timeout.

## Структура и зависимости

Один пакет internal/transport/http с именем httptransport. Созданные файлы:

| Файл | Ответственность |
| --- | --- |
| handler.go | Локальный интерфейс сервиса, конструктор, три endpoint-handler |
| router.go | ServeMux, диспетчеризация методов, JSON 404/405 |
| dto.go | Внутренние request/response DTO и преобразования |
| decode.go | Ограниченное чтение тела, JSON-объект, обязательность/типы полей |
| response.go | Предварительное кодирование JSON и единое преобразование ошибок |
| middleware.go | Recovery и запись результата HTTP-запроса в slog |
| *_test.go | httptest и ручной fake сервиса; без БД |

Делить на подпакеты handler/middleware пока не нужно. Директории и файлы
созданы вместе с реализацией; пустых слоёв нет.

Публичный контракт пакета:

```go
// package httptransport
// context, http и slog — стандартная библиотека;
// domain — kasperski-go-test/internal/domain.
type ThreatService interface {
    GetByMD5(ctx context.Context, md5 string) (domain.Threat, error)
    Create(ctx context.Context, md5 string, data domain.ThreatData) (domain.Threat, error)
    Update(ctx context.Context, md5 string, data domain.ThreatData) (domain.Threat, error)
}

func NewHandler(service ThreatService, logger *slog.Logger, maxBodyBytes int64) http.Handler
```

Интерфейс принадлежит HTTP-потребителю; реальный usecase.ThreatService
удовлетворяет ему неявно. Transport зависит только от domain и своего
интерфейса, не от конкретного сервиса. Конструктор требует ненулевые
service/logger и положительный maxBodyBytes, не выполняет I/O.
При nil зависимости или неположительном лимите конструктор паникует
до обработки запросов; конфигурация должна проверяться composition root.

## Маршруты

| Путь | Метод | Сервисный вызов | Успех | Ошибки операции |
| --- | --- | --- | --- | --- |
| /info/{md5} | GET | GetByMD5(ctx, pathMD5) | 200 + ThreatResponse | 400, 404, 500 |
| /info | POST | Create(ctx, bodyMD5, data) | 201 + ThreatResponse | 400, 409, 500 |
| /info/{md5} | PUT | Update(ctx, pathMD5, data) | 200 + ThreatResponse | 400, 404, 500 |

Параметр md5 извлекать через r.PathValue("md5"); передавать сервису как
строку после стандартного URL-декодирования маршрутизатором, без trim,
повторного unescape или lowercase. Не ограничивать route регулярным
выражением: /info/bad должен дойти до сервиса и дать 400, не router-404.
PUT не берёт MD5 из тела. Параметры query не используются.

Использовать стандартный http.ServeMux с path-only регистрациями /info,
/info/{md5} и fallback /. В обработчике каждого ресурса явно выбирать
разрешённый метод. Это даёт контроль над JSON 405 и не включает HEAD
неявно через специальную семантику шаблона "GET /...".

Проектные решения маршрутизации, не новые бизнес-операции:

- Неизвестный путь → 404 с JSON message/details, сервис не вызывается.
- Известный путь, неподдержанный метод → 405 с JSON и Allow: POST для
  /info либо Allow: GET, PUT для /info/{md5}. Сервис не вызывается.
- HEAD/OPTIONS/DELETE и другие методы отдельно не реализуются. HEAD даёт
  405 с Allow, но без тела по семантике HEAD; сервис Get не вызывается.
- /info/ и /info/{md5}/ не становятся новыми endpoint; для обычных таких
  путей действует fallback 404. Не добавлять redirect со slash-версии вручную.
- Стандартные redirect ServeMux при очистке неканонического URL (например,
  // или /./) сохраняются; это протокольное поведение stdlib, не JSON-ошибка API.

405 относится к неописанным методам и не меняет коды трёх операций OpenAPI.
Пояснение этой политики добавлено в info.description OpenAPI; пути,
схемы и коды трёх операций сохранены. Не добавлять health, metrics,
Swagger UI, авторизацию, CORS или новые бизнес-маршруты без отдельной задачи.

## Разбор запроса POST/PUT

Порядок: маршрут/метод → проверка актуальности context → Content-Type →
ограниченное чтение → JSON/обязательные поля → DTO → один вызов сервиса.
GET тело не разбирает; на сервис передаёт только MD5 из пути.

1. Content-Type разбирать через mime.ParseMediaType; принимать
   application/json и application/json; charset=utf-8 (регистр значения
   charset несущественен). Неизвестный charset, отсутствующий/некорректный
   Content-Type или другой media type → 400. Иные корректные параметры
   media type не используются. Accept не вводит negotiation/406.
2. Поддерживается обычное JSON-тело, без распаковки Content-Encoding;
   непустой Content-Encoding, отличный от identity, → 400.
3. Оборачивать Body в http.MaxBytesReader. Проектный default лимита —
   1 MiB (1 048 576 байт), передаётся из будущей конфигурации, а не читается
   из окружения в handler. Это лимит всего тела, не maxLength отдельных полей.
   Лимит должен работать и без Content-Length, в том числе с chunked body.
4. Прочитать ограниченное тело целиком; ошибка размера/чтения → 400, если
   запрос не отменён. Тело закрыть. До сервиса запрос не доходит.
5. Проверить UTF-8 исходных байтов и разобрать ровно один JSON-объект.
   Пустое тело, whitespace-only, null, массив/скаляр, обрезанный JSON,
   второй документ и мусор после объекта → 400. Пробелы после объекта допустимы.
6. Разобрать объект в map[string]json.RawMessage. Известные имена проверять
   точно и с учётом регистра: MD5/Name не заменяют обязательные md5/name.
   Не использовать DisallowUnknownFields: контракт разрешает неизвестные поля.
7. Для POST обязательны md5/name; для PUT — name. Отсутствие → 400.
   У каждого известного поля до декодирования строки явно исключить JSON
   null. Только затем декодировать string; иной тип → 400.
8. description отсутствует → nil; строка → указатель на значение, в том
   числе ""; JSON null → 400. Не подменять эти состояния друг другом.
9. Неизвестные поля игнорировать независимо от типа, включая created_at,
   updated_at и md5 в PUT; они не передаются сервису.

Внутренние request DTO: createRequest (MD5 string, Name string,
Description *string) и updateRequest (Name string, Description *string).
Проверки присутствия/null выполняются по RawMessage до заполнения DTO:
обычный *string не отличает null от отсутствия, а string — отсутствие от "".

Для повторяющихся JSON-ключей сохраняется поведение encoding/json v1:
для RawMessage в map используется последнее значение, затем оно валидируется.
Не обещать отклонение дубликатов или дополнительную Unicode-канонизацию,
которых этот декодер не обеспечивает; escape-последовательности разбираются
по правилам encoding/json. Это явный технический выбор, не исходное требование.

Ошибки размера и media type пока представлены существующим 400
«некорректный запрос». Не вводить 413/415 молча: если потребуются отдельные
коды, сначала изменить OpenAPI/требования. Default лимита, Content-Type
и Content-Encoding — решения текущего HTTP-проекта (ADR-012), а не цитаты ТЗ.

## Вызов сервиса и ответ

Handler переводит DTO в domain.ThreatData, вызывает сервис один раз и
не выполняет собственного поиска, слияния или повторной записи.
PUT без description передаёт nil: очистка уже определена контрактом.

Успешный ответ — отдельный threatResponse с JSON-тегами:

| Поле | Go-представление DTO | JSON |
| --- | --- | --- |
| md5 | string | Всегда присутствует, канонический MD5 результата сервиса |
| name | string | Всегда присутствует, включая пустую строку |
| description | *string с omitempty | nil опускается; указатель на "" остаётся "" |
| created_at | time.Time после UTC() | RFC 3339 с доступной дробной частью, суффикс Z |
| updated_at | time.Time после UTC() | Тот же формат, не локальное время |

Domain-структуры не получают JSON-теги и не кодируются напрямую.
Не обрезать строки, не вычислять даты, не подставлять описание/default.
POST возвращает тот же ThreatResponse с 201; дополнительных Location/ETag
и оболочек data/result контракт не требует.

Единый JSON writer предварительно сериализует DTO до WriteHeader, затем
задаёт Content-Type: application/json и нужный статус. Не использовать
http.Error: он формирует другой тип тела. Ошибка кодирования до отправки
заголовков → безопасный JSON 500. Ошибка сетевой записи после начала ответа
не исправляется второй попыткой WriteHeader или вторым JSON-документом.

## Ошибки

errorResponse всегда содержит строковые message и details без omitempty.
Сопоставлять domain-ошибки через errors.Is, не сравнивать error.Error().

| Причина | Статус | message | details |
| --- | --- | --- | --- |
| Форма/тип/обязательность JSON, media type, размер тела | 400 | Invalid request | Безопасное описание правила, без тела/значения пользователя |
| domain.ErrInvalidMD5 | 400 | Invalid MD5 | Требуются 32 hex-символа |
| domain.ErrInvalidThreatData | 400 | Invalid threat data | Недопустимое значение известного поля, без его содержимого |
| domain.ErrThreatNotFound в GET/PUT | 404 | Threat not found | Пустая строка допустима |
| domain.ErrThreatAlreadyExists в POST | 409 | Threat already exists | Пустая строка допустима |
| Неизвестный маршрут | 404 | Route not found | Пустая строка |
| Неподдержанный метод | 405 | Method not allowed | Пустая строка; заголовок Allow обязателен |
| Иная ошибка / неожиданная комбинация ошибки и операции | 500 | Internal server error | Пустая строка |

Не отдавать err.Error() технической ошибки, SQL, DSN, stack trace или секреты
в message/details. Например, NotFound от Create или AlreadyExists от Get
не превращать в новый для этой операции код: это внутренний сбой контракта, 500.

Правила context:

- Уже отменённый r.Context() или ошибка context.Canceled при отменённом
  request context: не вызывать повторно сервис и не пытаться доставить новый
  JSON-ответ отключившемуся клиенту. Не придумывать статус 499.
- DeadlineExceeded при доступном writer → generic JSON 500; 504 не вводится
  без изменения контракта. Ошибка Canceled при ещё активном r.Context()
  также является внутренней ошибкой, 500.
- Если сервис вернул успех, не заменять его поздней проверкой ctx.Err();
  предпринять обычную запись ответа. Её сетевой сбой не означает rollback.

Ошибки HTTP-парсинга самого net/http до handler и недоступное соединение
не обязаны иметь JSON-тело: transport не может гарантировать доставку ответа.

## Middleware и логирование

Достаточно recovery и structured request logging на slog, в том же пакете.
Не добавлять rate limiting, API key, CORS, request-id или сторонний middleware
framework по умолчанию.

Порядок: request logging → recovery → router → handler. Один итоговый
request log с method, шаблоном маршрута, отправленным status и duration;
если ответа не было из-за отмены, статус не выдумывать, отметить отмену.
Не логировать сырые body, DSN, секреты или значения JSON-полей.

Неожиданная ошибка логируется один раз на HTTP-границе с причиной;
обычные 400/404/409 не требуют Error-логирования. Итоговый access log
не повторяет error/stack. Сервис и repository повторно ошибку не логируют.
Recovery даёт generic JSON 500 при panic до начала ответа. После начала
ответа не дописывать JSON/новый статус: прервать ответ через стандартный
http.ErrAbortHandler. Этот sentinel не перехватывать как обычный panic
и не дублировать его логирование net/http.

Для status/response-start tracking достаточно небольшого ResponseWriter
декоратора с Unwrap; не превращать его в универсальную библиотеку.
Текущий API не использует streaming, hijacking или WebSocket. Проверять
поведение обычных Write/WriteHeader и не заявлять необеспеченные интерфейсы.

## План тестирования и критерии готовности

HTTP unit tests — testing/httptest и ручной fake локального ThreatService.
Никаких DB, нового mocking-пакета и доступа handler к repository.

| Проверка | Ожидаемый результат |
| --- | --- |
| Три успешные операции | 200/201/200, точная форма ThreatResponse и application/json |
| md5 пути/тела, context | Переданы сервису без нормализации, один вызов правильного метода |
| Некорректный MD5 | Через настоящий сервис — 400, а не router-404 |
| Пустое/обрезанное тело, null, массив/скаляр, два документа | 400; fake сервиса не вызван |
| Отсутствие md5/name, null и неверный тип известных полей | 400 до сервиса |
| Отсутствие/пустое description | nil/указатель на "" переданы без подмены |
| Неизвестные поля и даты | Игнорируются, в том числе нестроковые значения и md5 в PUT |
| Case-sensitive имена JSON, повторные ключи | Обязательные имена точные; последнее повторное значение валидируется |
| Лимит и media type | 400 до сервиса, включая chunked/нет Content-Length; JSON с UTF-8 charset принимается |
| Поля ответа | Пустое name присутствует, nil description опущен, пустое — присутствует; даты UTC |
| Обёрнутые ошибки сервиса | Правильные коды через errors.Is; технические детали отсутствуют в ответе |
| Неизвестный путь/неверный метод/HEAD | JSON 404/405 и Allow; HEAD без тела и без вызова Get |
| Уже canceled request, ошибки context, поздняя отмена | Соответствие правилам выше, нет retries или вымышленного 499/504 |
| Panic и ошибка записи | 500 только до начала ответа; после начала нет второго заголовка/JSON |
| Логи | Неожиданная причина один раз, нет секретов/body, access log без повторного error |

Отсутствие body у HEAD и поведение оборванного/частично записанного ответа
проверять также через httptest.Server и реальный HTTP-клиент: один
ResponseRecorder не воспроизводит сетевые правила net/http.

После unit tests добавить HTTP → настоящий ThreatService → pgx → PostgreSQL
integration tests на отдельной БД с миграцией; проверить 200/201/400/404/409/500,
дубликат в ином регистре, очистку описания и отсутствие upsert. Нынешние
repository/service integration tests сами по себе не считаются HTTP-покрытием.
HTTP-проверка уже добавлена: make test-integration выполняет 13 Go integration
tests с -race, включая настоящий HTTP-клиент и PostgreSQL.
make test-repository сохранён как алиас.

## Границы этапа и следующий шаг

Transport, конфигурация, сервер и wiring реализованы. Подробности
старта/остановки и smoke tests — [RUNTIME.md](RUNTIME.md). В OpenAPI добавлены только пояснения HTTP-политики;
сервис, repository, миграции и зависимости не менялись.

Runtime-план реализован: pgxpool, net.Listen/http.Server, graceful shutdown,
Compose API и проверки настоящего бинарника/контейнера. HTTP timeouts и
лимит настраиваются окружением; http.TimeoutHandler не используется.
Новых endpoints и бизнес-операций не добавлено.

Технические источники:

- [net/http: ServeMux, PathValue и ResponseWriter](https://pkg.go.dev/net/http).
- [Ограничение чтения тела](https://pkg.go.dev/net/http#MaxBytesReader).
- [encoding/json: RawMessage и поведение Unmarshal](https://pkg.go.dev/encoding/json).
- [Разбор media type](https://pkg.go.dev/mime#ParseMediaType).
