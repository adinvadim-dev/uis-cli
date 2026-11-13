## Что нового

- Навык UIS считает нажатия клавиш по структурным событиям карточек звонков (`call_brief.py dtmf`): с дедупликацией, маскировкой номеров и отдельным списком неудачных чтений вместо нулей. Добавлен рецепт подсчётов за период через `--all`, поиск звонков робота по номеру сотрудника и правило, когда сторону можно называть «роботом».

The UIS skill counts keypresses from structured call-detail events with deduplication, masked parties and explicit failed captures, and documents bounded period counts, employee-number search for robot lines and evidence required before labelling a party a robot.
