# Запуск и жизненный цикл API

Реализовано 2026-11-05, ADR-013. Composition root находится в cmd/api;
internal/config читает только окружение, без автоматической загрузки .env.
Существующие [HTTP](HTTP.md), [service](SERVICE.md) и [repository](REPOSITORY.md)
собираются конструкторами без новых зависимостей и бизнес-операций.

## Конфигурация

| Переменная | Default | Проверка/назначение |
| --- | --- | --- |
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

Default используется только при отсутствии переменной. Явно пустые,
некорректные или содержащие NUL значения отклоняются. Ошибка содержит имя
переменной, не её значение. Config не имеет String-метода; не логировать
структуру целиком. URL подключения строится net/url с UserPassword;
пароль с @, :, /, ?, #, %, пробелами не требует ручного экранирования.
URL не выводится в логи. Ошибки разбора pgx-конфигурации возвращаются без
сырой строки подключения. В runtime image добавлен системный CA bundle.

HTTP timeouts относятся к чтению/записи/keep-alive net/http; они не являются
скрытым timeout сервисных операций и не меняют ошибки API на 499/504.
TLS HTTP, отдельные production-роли и новые health endpoints не добавлены.

## Запуск

1. Load и проверка всей конфигурации, создание JSON slog.
2. signal.NotifyContext для SIGINT/SIGTERM, создание caller-owned pgxpool.
3. В одном startup context с POSTGRES_CONNECT_TIMEOUT выполнить Ping,
   проверить единственную миграционную версию 1 с dirty=false и доступность
   SELECT полей public.threats. Отсутствие/иная версия/dirty/нет таблицы —
   ошибка с указанием make migrate-up. Никакого DDL/автоматического force.
4. Создать repository → ThreatService → NewHandler, открыть net.Listen.
5. Запустить http.Server с заданными таймаутами. JSON INFO-событие
   "HTTP server listening" содержит фактический address, в том числе для :0.

При добавлении следующей SQL-миграции совместимость версии схемы должна
быть пересмотрена явно; текущий бинарник требует ровно версию 1.

## Остановка

serveHTTP владеет listener и контекстами запросов, runServer — пулом.
Контекст BaseContext создан отдельно от контекста сигнала: SIGTERM сначала
закрывает listener через Shutdown, не отменяя текущие запросы.
Главный goroutine ждёт завершения Shutdown, не только возврата Server.Serve.

На штатном drain запросы завершаются, listener освобождается, затем закрывается
пул. Если shutdown budget истёк, отменяются request contexts, вызывается
Server.Close и возвращается ошибка. Новых попыток записи/транзакций нет.

Exit 0 — штатная остановка по SIGINT/SIGTERM, включая отмену startup.
Exit 1 — конфигурация, подключение/схема, listener, ошибка Serve или истечение
shutdown timeout. Fatal-события логируются один раз JSON в stderr независимо
от LOG_LEVEL; request/lifecycle logs идут через настроенный logger в stdout.
ErrorLog самого net/http направлен в slog. Пароли/DSN не логируются.

## Compose

API слушает 0.0.0.0:8080 внутри контейнера и обращается к postgres:5432.
Эти два адреса задаются Compose отдельно от настроек локального бинарника.
API_PORT (default 8080) меняет только порт публикации API на 127.0.0.1;
POSTGRES_PORT — только публикацию БД на хосте. Постоянный том сохраняется.

make up последовательно выполняет db-up с ожиданием healthcheck,
migrate-up и compose up -d --build api. Мигратор остаётся одноразовым
сервисом tools; сам API ничего не мигрирует. compose up api без подготовленной
схемы завершается ошибкой startup. Успешное открытие listener видно в make logs.

make down удаляет контейнеры/сеть, сохраняя том. make logs показывает логи API.
make docker-run — алиас make up. Compose stop_grace_period равен 30s;
при увеличении HTTP_SHUTDOWN_TIMEOUT увеличить grace period с запасом.
POSTGRES_USER используется как существующая локальная административная роль;
разделение production-прав остаётся вне текущего MVP.

## Проверки

- Config unit tests: defaults, unset/empty, duration/integer boundaries,
  SSL modes, адреса, slog levels и URL со специальными символами реквизитов.
- Lifecycle unit tests: остановка без запросов, drain без преждевременной
  отмены context, принудительное закрытие по timeout и ошибка listener.
- make test-runtime: отдельная PostgreSQL и скомпилированный настоящий
  бинарник, ошибки config/DB/schema/порта, POST/GET/PUT, SIGTERM/SIGINT,
  освобождение порта; затем make up/down с Docker build, HTTP и persistence.
- make test-integration: существующие 13 тестов HTTP/service/repository на БД.
- make test-db: миграции up/down/up и constraints отдельно от приложения.

Runner использует отдельные случайные проект/порты/том, не читает локальную
.env, очищает влияющие на приложение переменные тестового окружения и в
finally удаляет только свои контейнеры, сеть и том. Ошибки startup проверяются
без вывода пароля. Docker build cache может оставаться для следующих сборок.
