# AlisaAI

Базовый Go-бэкенд с HTTP-сервером и местом для интеграций с внешними сервисами.
База данных не требуется. Для загрузки `.env` используется библиотека `godotenv`.

## Структура

```text
cmd/api/                     точка входа, запуск и связывание зависимостей
internal/config/             настройки из переменных окружения
internal/transport/httpapi/   маршруты и HTTP-обработчики
internal/service/            прикладные сценарии
internal/integrations/       клиенты внешних сервисов
.env.example                 пример настроек
Makefile                     команды разработки
```

Запрос проходит через HTTP-обработчик → прикладной сервис → клиент интеграции.
Первая интеграция — TickTick: `service/ticktick.go` собирает задачи,
а `integrations/ticktick` выполняет запросы к Open API.

## Запуск

Требуется Go 1.26 или новее.

```sh
make run
```

Сейчас доступен только `POST /api/v1/alice/webhook`. Остальные маршруты,
включая `/healthz`, `/api/v1/ticktick/*` и `/api/v1/gigachat/chat`, временно
отключены и возвращают `404`. Их обработчики сохранены в коде.

Проверка запуска навыка без обращения к внешним API:

```sh
curl http://localhost:8080/api/v1/alice/webhook \
  -H 'Content-Type: application/json' \
  -d '{"version":"1.0","session":{"session_id":"test","skill_id":"test","new":true},"request":{"type":"SimpleUtterance","command":"","original_utterance":""}}'
```

Ответ содержит приветствие в `response.text`, `end_session: false` и
`version: "1.0"`.

## Webhook Алисы

В Яндекс Диалогах укажите Webhook URL:
`https://ВАШ-ДОМЕН-NGROK/api/v1/alice/webhook`.
Туннель должен вести на порт локального сервера; например, для порта 18080:

```sh
HTTP_ADDR=127.0.0.1:18080 go run ./cmd/api
# В другом терминале:
ngrok http 18080
```

Обработчик принимает протокол `1.0` и запросы `SimpleUtterance`. Поля
`meta.timezone` задаёт часовой пояс для текущей даты в системном промпте;
при отсутствии или некорректном значении используется `Europe/Moscow`.
Остальные поля `meta`, NLU и состояние допускаются и игнорируются. Текст из `request.command`
передаётся GigaChat; для пустой команды в продолжающейся сессии используется
`original_utterance`. Запуск без команды, `ping`, помощь и выход обрабатываются
локально. Команда в первом запросе сессии выполняется сразу. Неизвестные типы
событий получают предложение задать вопрос голосом или текстом.

Пример запроса к модели:

```sh
curl --max-time 5 http://localhost:8080/api/v1/alice/webhook \
  -H 'Content-Type: application/json' \
  -d '{"version":"1.0","session":{"session_id":"test","skill_id":"test","new":false},"request":{"type":"SimpleUtterance","command":"сколько задач у меня во входящих тиктик"}}'
```

Формат ответа:

```json
{"version":"1.0","response":{"text":"У вас 11 задач.","end_session":false}}
```

Число выше — пример, реальные задачи получаются через инструмент TickTick.
Текст ограничен 1024 символами с корректным усечением Unicode. На обработку
отводится 3,5 секунды, включая вызовы GigaChat и TickTick: это оставляет запас
на сеть в пределах лимита Алисы 4,5 секунды. Контекст отменяет запросы к
провайдерам; при таймауте или ошибке возвращается HTTP 200 с коротким сообщением
для пользователя. Завершённый результат при таймауте не сохраняется:
следующая реплика запускает новый запрос. История диалога пока не хранится.

Невалидный JSON/протокол даёт `400`, тело свыше 64 KiB — `413`, неверный
Content-Type — `415`; допустимый метод — POST. Ошибки провайдера и секреты
не попадают в ответ. `session.skill_id` проверяется на наличие, но не является
авторизацией: привязка к конкретному пользователю и проверка происхождения
webhook пока не реализованы. Любой имеющий доступ к webhook может обратиться
к задачам владельца настроенного токена.

Документация Яндекса: [запрос](https://yandex.ru/dev/dialogs/alice/doc/ru/request),
[ответ и ограничения](https://yandex.ru/dev/dialogs/alice/doc/ru/response).

## Настройки

| Переменная | По умолчанию | Назначение |
| --- | --- | --- |
| `HTTP_ADDR` | `:8080` | Адрес HTTP-сервера |
| `SHUTDOWN_TIMEOUT` | `10s` | Время ожидания активных запросов при остановке |
| `TICKTICK_ACCESS_TOKEN` | пусто | Персональный API-токен или OAuth access token TickTick |
| `TICKTICK_TIMEOUT` | `10s` | Общий бюджет времени операции TickTick, включая сбор задач из проектов |
| `GIGACHAT_AUTH_KEY` | пусто | Ключ авторизации из кабинета GigaChat, без префикса `Basic` |
| `GIGACHAT_SCOPE` | `GIGACHAT_API_PERS` | Версия API для OAuth |
| `GIGACHAT_MODEL` | `GigaChat-2-Pro` | Имя модели |
| `GIGACHAT_BASE_URL` | `https://api.giga.chat/v1` | Базовый URL API |
| `GIGACHAT_AUTH_URL` | `https://ngw.devices.sberbank.ru:9443/api/v2/oauth` | URL получения токена |
| `GIGACHAT_TIMEOUT` | `60s` | Общий бюджет диалога, включая OAuth, генерацию и инструменты; также таймаут HTTP-клиента |
| `GIGACHAT_CA_FILE` | пусто | Дополнительные доверенные CA в PEM; добавляются к системным сертификатам |

Файл `.env` автоматически загружается из текущей рабочей папки при старте.
Уже заданные переменные окружения имеют приоритет, даже если они пустые.
Отсутствие `.env` допустимо; ошибка чтения или синтаксиса останавливает запуск.
Для локального запуска создайте `.env`, заполните настройки и запустите сервис:

```sh
cp .env.example .env
make run
```

Также настройки можно передать непосредственно при запуске:

```sh
HTTP_ADDR=127.0.0.1:9000 go run ./cmd/api
```

## TickTick

1. Откройте [TickTick Web](https://ticktick.com/webapp/), затем
   Settings → Account → API Token и создайте персональный токен.
2. Запишите его в локальный `.env` как `TICKTICK_ACCESS_TOKEN=...`.
   Файл исключён из Git. Запустите сервер командой `make run` из корня проекта.
3. Запросите задачи голосом через навык или через сервис из Go-кода.

Прямые HTTP-маршруты TickTick временно отключены. Сервис `ListInboxTasks`
продолжает обращаться к `GET /open/v1/project/inbox/data` по служебному
идентификатору `inbox`, не ищет проект по имени. Пользовательский проект
с названием Inbox — другой список. Любая ошибка провайдера прерывает сбор;
частичный список не выдаётся.

Для подключения других пользователей потребуется OAuth с правом `tasks:read`;
текущая версия использует один токен из окружения, без БД и хранения сессий.
Подробности: [официальная документация TickTick](https://developer.ticktick.com/docs/index.html#/openapi).

## GigaChat

`internal/integrations/gigachat` — HTTP-клиент с OAuth, кешированием токена
и обновлением перед истечением срока действия. После HTTP 401 клиент один раз
обновляет токен и повторяет запрос. Другие ошибки автоматически не повторяются.
Проверка TLS включена; при необходимости укажите доверенный CA-файл через
`GIGACHAT_CA_FILE`. Тела ошибок провайдера и ключи не включаются в ошибки клиента.

`service.GigaChat.Chat(ctx, prompt)` выполняет отдельный диалог без сохранения
истории. Модели доступен инструмент `get_ticktick_inbox_tasks` без аргументов:
он вызывает существующий `TickTick.ListInboxTasks` для системных «Входящих».
При вызове инструмента содержимое задач передаётся GigaChat в JSON-объекте
`{"count":11,"tasks":[...]}`; `count` вычисляется сервисом, пустой список —
`{"count":0,"tasks":[]}`. Сервис сохраняет
`function_call` и `functions_state_id` при передаче результата модели.
Неизвестные функции и непустые аргументы отклоняются. Максимум — три вызова
инструмента на диалог; ошибка TickTick прерывает диалог без выдуманного ответа.

Системный промпт находится в `internal/service/gigachat_prompt.txt` и включается
в бинарник через `go:embed`: после правки нужен перезапуск через `go run` или
пересборка. В начало каждого запроса подставляются текущее время, даты сегодня,
вчера и завтра и часовой пояс. Правила требуют отличать задачи на сегодня,
просроченные и задачи без даты, учитывать `status`, не имитировать результаты
инструментов и не ссылаться на отсутствующую историю диалога.

Инструмент добавляет к каждой задаче `dateHints`: календарные даты начала и
срока, их отношение к сегодняшнему дню и использованный часовой пояс.
Для задач на весь день берётся корректный `timeZone` задачи, для остальных —
часовой пояс пользователя. Исходные поля и все задачи сохраняются, фильтрация
в Go пока не выполняется. Подсказки и промпт не гарантируют правильного отбора
моделью: в живой проверке GigaChat всё ещё включал будущую задачу в ответ про
сегодня и иногда отвечал без вызова инструмента.

Поведенческая проверка с реальной моделью и синтетическими задачами с известными
датами (потребляет квоту GigaChat; ошибки модели приводят к провалу проверки):

```sh
ALISAAI_LIVE_TEST=1 go test ./cmd/api -run TestGigaChatPromptLive -v -count=1
```

Прямая ручка `POST /api/v1/gigachat/chat` временно отключена. HTTP-доступ
к сервису теперь только через webhook Алисы, описанный выше. Вызовы из Go
по-прежнему доступны. `GIGACHAT_TIMEOUT` задаёт общий таймаут сервиса, но при
запросе Алисы действует более короткий дедлайн обработчика — 3,5 секунды.

Если новый адрес API возвращает `404`, для прежних подключений можно задать
`GIGACHAT_BASE_URL=https://gigachat.devices.sberbank.ru/api/v1`.
После изменения `.env` перезапустите сервер.

Реальная проверка webhook Алисы с GigaChat и TickTick (расходует квоту API
и передаёт задачи модели; обычные тесты её пропускают):

```sh
ALISAAI_LIVE_TEST=1 go test ./cmd/api -run TestAliceLive -v -count=1
```

Проверка требует вызова Inbox-инструмента и сверяет число в ответе модели
с фактическим числом полученных задач.

Подключение из Go-кода внутри этого модуля (после `config.Load()`):

```go
client, err := gigachat.NewClient(gigachat.Options{
    AuthKey: cfg.GigaChatAuthKey,
    Scope: cfg.GigaChatScope,
    BaseURL: cfg.GigaChatBaseURL,
    AuthURL: cfg.GigaChatAuthURL,
    CAFile: cfg.GigaChatCAFile,
    Timeout: cfg.GigaChatTimeout,
})
if err != nil {
    return err
}
tasks := service.NewTickTick(
    ticktick.NewClient(cfg.TickTickToken, cfg.TickTickTimeout),
    cfg.TickTickTimeout,
)
assistant := service.NewGigaChat(client, tasks, cfg.GigaChatModel, cfg.GigaChatTimeout)
answer, err := assistant.Chat(ctx, "Какие задачи у меня во входящих TickTick?")
```

Импорты: `alisaai/internal/integrations/gigachat`,
`alisaai/internal/integrations/ticktick`, `alisaai/internal/service`.
Без `GIGACHAT_AUTH_KEY` вызов возвращает `gigachat.ErrNotConfigured`,
но существующий HTTP-сервер запускается как прежде.

Контракт соответствует официальной документации:
[авторизация](https://developers.sber.ru/docs/ru/gigachat/api/reference/rest/gigachat-api),
[вызов пользовательских функций](https://developers.sber.ru/docs/ru/gigachat/guides/functions/generating-arguments-for-custom-functions).

## Разработка

```sh
make fmt
make vet
make test
make build
```

Собранный сервер: `./bin/api`. Логи выводятся в stdout в формате JSON.
При SIGINT или SIGTERM сервер ожидает завершения активных запросов в пределах
`SHUTDOWN_TIMEOUT`.

Модуль пока называется `alisaai`. Когда появится удалённый репозиторий,
замените имя в `go.mod` и внутренних импортах на путь репозитория.
