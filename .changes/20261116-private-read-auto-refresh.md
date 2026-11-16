## Что нового

- Приватные чтения UIS сами восстанавливают истёкшую сессию кабинета: при настроенном credential_process CLI один раз выполняет то же обновление, что `auth refresh --surface private --no-input`, и повторяет чтение. Записи, несовпадение принципала и `UIS_NO_AUTO_REFRESH=1` автообновление не запускают; при неудаче остаётся исходная ошибка exit 3 с `details.auto_refresh`.

Private reads now refresh an expired private session once in-process (when a credential_process is configured) and retry the read, so scheduled callers no longer stall until someone runs `auth refresh --surface private --no-input` by hand. Writes, principal mismatch and `UIS_NO_AUTO_REFRESH=1` never refresh; a failed attempt keeps the original exit 3 with `error.details.auto_refresh`.
