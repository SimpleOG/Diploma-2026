# Дипломная работа: Система мгновенного обмена сообщениями

Три варианта архитектуры чат-системы на Go для сравнительного анализа производительности.

---

## Варианты

| | Вариант А | Вариант Б | Вариант В |
|---|---|---|---|
| Архитектура | Монолит | Монолит | Микросервисы |
| Обработка | Синхронная | Асинхронная (Kafka) | RabbitMQ |
| БД сообщений | PostgreSQL | MongoDB | MongoDB |
| Кеш | Redis | Memcached + Redis | Redis |
| Порт API | 8080 | 8081 | 8081–8084 |

---

## Запуск

### Вариант А

```bash
cd variant-a
docker-compose up --build -d
```

API: `http://localhost:8080`

### Вариант Б

```bash
cd variant-b
docker-compose up --build -d
```

API: `http://localhost:8081`

### Вариант В

```bash
cd variant-c
docker-compose up --build -d
```

Сервисы:
- Auth: `http://localhost:8081`
- Rooms: `http://localhost:8082`
- Messages: `http://localhost:8083`
- WebSocket: `ws://localhost:8084/ws`
- RabbitMQ UI: `http://localhost:15672` (guest / guest)

---

## Тестирование через curl

Примеры для Варианта А (для Б/В замените порт):

```bash
# Регистрация
curl -s -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username":"alice","password":"secret123"}' | jq .

# Логин
TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"alice","password":"secret123"}' | jq -r .token)

# Создать комнату
ROOM=$(curl -s -X POST http://localhost:8080/api/v1/rooms \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"general"}' | jq -r .id)

# Вступить в комнату
curl -s -X POST http://localhost:8080/api/v1/rooms/$ROOM/join \
  -H "Authorization: Bearer $TOKEN"

# История сообщений
curl -s "http://localhost:8080/api/v1/rooms/$ROOM/messages" \
  -H "Authorization: Bearer $TOKEN" | jq .

# Healthcheck
curl -s http://localhost:8080/health
```

### WebSocket (websocat)

```bash
# Установка: https://github.com/vi/websocat
websocat "ws://localhost:8080/ws?token=$TOKEN"

# Вступить в комнату
{"type":"join","room_id":"<ROOM_ID>"}

# Отправить сообщение
{"type":"message","room_id":"<ROOM_ID>","content":"Привет!"}
```

---

## Нагрузочные тесты

```bash
# Установка k6
brew install k6          # macOS
sudo snap install k6     # Linux

# Запуск
cd load-test
k6 run --env BASE_URL=http://localhost:8080 scenario_a.js
k6 run --env BASE_URL=http://localhost:8081 scenario_b.js
k6 run scenario_c.js
```

Подробнее — в `load-test/README.md`.

---

## Конфигурация

Каждый сервис читает конфиг из файла `app.env` в своей директории.
Файлы уже заполнены значениями для docker-compose — редактировать не нужно.
