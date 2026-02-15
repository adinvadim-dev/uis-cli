---
name: uis-cli
description: >
  CLI для UIS Data API (JSON-RPC): человеческие команды `uis <resource> <action>`
  (генерируются из спека публичной документации), плюс низкоуровневый `uis call`
  как escape hatch.
---

# uis-cli

## Назначение

- Для людей: `uis <resource> <action> ...` с нормальными именами и help.
- Для автоматизации/AI: всё равно остаётся JSON-вывод результата и `uis call` для точного контроля.

## Установка/сборка

```bash
make build
bin/uis --help
```

## Аутентификация

Токен никогда не принимается через флаги (чтобы не утекал в history/process list).

Варианты:

- env: `UIS_TOKEN=...`
- сохранить в конфиг: `uis auth login` (интерактивно) или `uis auth login --token-stdin`

Команды:

```bash
uis auth login
uis auth login --token-stdin < token.txt
uis auth status
uis auth logout
```

## Конфиг и приоритеты

Приоритет (выше побеждает ниже):

1. флаги
2. env
3. config file
4. defaults

Env:

- `UIS_CONFIG` путь к конфигу
- `UIS_TOKEN` токен (если задан, перекрывает токен в конфиге)
- `UIS_HOST` (default: `dataapi.uiscom.ru`)
- `UIS_API_VERSION` (default: `v2.0`)
- `UIS_TIMEOUT` (например `60s`)
- `UIS_RETRIES` (целое число)

Команды:

```bash
uis config path
uis config show --json
uis config set --host dataapi.uiscom.ru --api-version v2.0 --timeout 60s --retries 3
```

## Вызов методов

### Человеческие команды (рекомендуется)

```bash
uis campaigns list --help
uis campaigns list --limit 10
uis contacts list --help
```

По умолчанию вывод читаемый (таблица/ключ-значение). Чтобы получить JSON, добавьте `--json`:

```bash
uis --json available-virtual-numbers list
```

Проверка параметров без сетевого запроса:

```bash
uis --dry-run ct-summary-report get --date-from "2026-01-01 00:00:00" --date-till "2026-01-02 00:00:00"
uis --json --dry-run campaigns list --limit 10
```

### Низкоуровневый вызов (escape hatch)

```bash
uis call get.campaigns --param limit:=100
uis call get.campaigns --params-json '{"limit":100}'
```

`--param` поддерживает:

- `key=value` (строка)
- `key:=<json>` (числа/булевы/массивы/объекты)

Команды `uis <resource> <action>` генерируются из `internal/spec/data/api_spec.json`
в исходник `internal/cli/generated_resources.go`.

## Обновление спека (парсинг всей документации UIS)

Скрейпер обходит документацию, вытаскивает:

- `Метод`
- `Описание`
- таблицу `Параметры запроса`

Команды:

```bash
make spec
make gen
```

После этого пересоберите бинарь, чтобы новый `api_spec.json` попал в `go:embed`:

```bash
make build
```

## Коды выхода

- `0` успех
- `1` ошибка выполнения (I/O, внутренние ошибки)
- `2` неправильное использование (валидация аргументов/параметров)
- `10` проблемы аутентификации (нет токена, 401/403)
- `11` rate limit (HTTP 429)
- `12` ошибка API/HTTP (прочее)
