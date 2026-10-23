# upwatch

Сервис мониторинга доступности сайтов (uptime monitor) на Go и PostgreSQL.

## Стек

- **Go 1.25** — только стандартная библиотека для HTTP (`net/http` с роутингом Go 1.22+, `log/slog`)
- **PostgreSQL 17** — драйвер [pgx v5](https://github.com/jackc/pgx) (`pgxpool`)
- **goose** - миграции
- **Docker Compose** — локальная база

## Быстрый старт

Нужны Go 1.25+, Docker и goose:

```bash
go install github.com/pressly/goose/v3/cmd/goose@latest
```

Запуск:

```bash
cp .env.example .env
make up            # PostgreSQL в Docker (порт 5433)
make migrate-up    # схема БД
make run           # сервис на :8080
```

Проверка:

```bash
curl localhost:8080/healthz
# {"status":"ok"}
```

Все команды — `make help`. Готовый сценарий ручной проверки — [`api.http`](api.http) (VS Code + REST Client).

## API

| Метод | Путь | Описание | Успех |
| --- | --- | --- | --- |
| `GET` | `/healthz` | Проверка сервиса и БД | `200` / `503` |
| `POST` | `/api/v1/monitors` | Создать монитор | `201` |
| `GET` | `/api/v1/monitors` | Список мониторов | `200` |
| `GET` | `/api/v1/monitors/{id}` | Получить монитор | `200` |
| `PUT` | `/api/v1/monitors/{id}` | Полностью обновить монитор | `200` |
| `DELETE` | `/api/v1/monitors/{id}` | Удалить монитор | `204` |

### Пример

```bash
curl -X POST localhost:8080/api/v1/monitors \
  -H 'Content-Type: application/json' \
  -d '{"name": "Example", "url": "https://example.com", "interval_seconds": 60}'
```

```json
{
  "id": 1,
  "name": "Example",
  "url": "https://example.com",
  "interval_seconds": 60,
  "timeout_seconds": 10,
  "enabled": true,
  "created_at": "2026-10-23T14:00:00.000000+03:00",
  "updated_at": "2026-10-23T14:00:00.000000+03:00"
}
```

### Правила валидации

| Поле | Обязательное | Правило |
| --- | --- | --- |
| `name` | да | 1–100 символов |
| `url` | да | абсолютный `http` или `https` URL |
| `interval_seconds` | да | от 30 до 86400 |
| `timeout_seconds` | нет, по умолчанию 10 | от 1 до 60, меньше `interval_seconds` |
| `enabled` | нет, по умолчанию `true` | — |

### Ошибки

Все ошибки — JSON вида `{"error": "..."}`.

| Статус | Когда |
| --- | --- |
| `400` | некорректный JSON, неизвестное поле, неверный `id` |
| `404` | монитор не найден |
| `422` | не прошла валидация; в ответе `fields` с ошибкой по каждому полю |
| `500` | внутренняя ошибка; подробности только в логах сервера |

```json
{
  "error": "validation failed",
  "fields": {
    "name": "must not be empty",
    "url": "must be an absolute http(s) URL"
  }
}
```

## Конфигурация

Только через переменные окружения. `make` подхватывает их из `.env`.

| Переменная | По умолчанию | Описание |
| --- | --- | --- |
| `UPWATCH_DATABASE_URL` | — (обязательна) | строка подключения к PostgreSQL |
| `UPWATCH_HTTP_ADDR` | `:8080` | адрес HTTP-сервера |
| `UPWATCH_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `UPWATCH_SHUTDOWN_TIMEOUT` | `10s` | сколько ждать завершения запросов при остановке |
| `UPWATCH_TEST_DATABASE_URL` | — | база для интеграционных тестов |

## Архитектура

```text
cmd/upwatch/            точка входа: сборка зависимостей, graceful shutdown
internal/
  monitor/              доменная модель и валидация (без внешних зависимостей)
  config/               конфигурация из переменных окружения
  storage/postgres/     репозиторий на pgx
  httpapi/              роутинг, хэндлеры, middleware
migrations/             SQL-миграции goose
```

```text
cmd/upwatch ──► config
     │      ──► storage/postgres ──► monitor
     └─────────► httpapi ──────────► monitor
```

Ключевые решения:

- **HTTP-слой не зависит от базы.** `httpapi` работает с интерфейсом `MonitorStore`, объявленным на стороне потребителя. Хэндлеры тестируются с хранилищем в памяти, без Docker.
- **Валидация в двух местах.** В Go — для понятных ошибок клиенту, `CHECK`-ограничения в PostgreSQL — как последняя линия защиты данных.
- **Строгий разбор JSON.** Неизвестные поля и лишние данные в теле отклоняются, размер тела ограничен 1 МБ.
- **Внутренние ошибки не утекают клиенту.** Детали пишутся в лог, наружу уходит только `internal error`.
- **Graceful shutdown.** По SIGINT/SIGTERM сервер перестаёт принимать соединения, дожидается текущих запросов и только потом закрывает пул БД.
- **Таймауты HTTP-сервера** — защита от медленных клиентов (Slowloris).

## Тесты

```bash
make test               # юнит-тесты: валидация, конфиг, все хэндлеры (без БД)
make test-integration   # репозиторий против настоящего PostgreSQL
```

Перед интеграционными тестами: `make up && make migrate-test`. Они используют отдельную базу `upwatch_test` и очищают её перед каждым тестом.