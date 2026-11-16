## Что нового

- Новая команда `uis recording download <id> --output <путь>` скачивает запись разговора: звонок проверяется через детали кабинета, аудио пишется в явно указанный файл (по файлу на плечо, суффикс `.legN`), а в вывод попадают только метаданные — путь, размер, sha256, тип. Ссылки на запись не печатаются; существующий файл не перезаписывается без `--force`; нет записи — exit 5.

`uis recording download <id> --output <path>` downloads a call recording: the call is verified through private call details, audio is written to the explicit path (one file per leg with a `.legN` suffix) and only metadata is printed (path, bytes, sha256, content type). Media links never reach output; existing files need `--force`; a missing recording exits 5.
