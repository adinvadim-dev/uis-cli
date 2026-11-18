## Что нового

- `calls search --source private --all` получил `--slice <длительность>` (например `--slice 1h`): период читается последовательными закрытыми окнами, у каждого свой 150-секундный бюджет и один повтор при транспортной ошибке или «плавающей» выборке. Загруженный день теперь выгружается полностью одной командой вместо ошибки `cabinet_transport`; покрытие показывает окна, пустые окна и повторы, а сбой окна возвращает его границы без частичных данных.

`calls search --source private --all` accepts `--slice <duration>` (e.g. `--slice 1h`) and reads the window as consecutive closed sub-windows, each with its own 150-second budget and one retry on transport, response-size or unstable-population failures. A busy day now exports completely in one command instead of failing with `cabinet_transport`; coverage reports windows, empty windows, retries and per-window results, and a window that fails twice fails the command with its bounds and no partial data.
