# Дипломная работа: Система мгновенного обмена сообщениями

Три варианта архитектуры чат-системы на Go для сравнительного анализа производительности.

---

## Варианты

| | Вариант А | Вариант Б | Вариант В |
|---|---|---|---|
| Архитектура | Монолит | Монолит | Микросервисы |
| Обработка сообщений | Синхронная | Асинхронная (Kafka) | RabbitMQ |
| БД сообщений | PostgreSQL | MongoDB | MongoDB |
| Кеш членства | Redis | Memcached | Redis |
| WebSocket координация | Redis pub/sub | Redis pub/sub | Redis pub/sub |
| Порт API | 8080 | 8080 | 8081–8084 |

---

## Запуск

```bash
# Вариант А
cd variant-a && docker-compose up --build -d

# Вариант Б
cd variant-b && docker-compose up --build -d

# Вариант В
cd variant-c && docker-compose up --build -d
```

Конфиги уже заполнены в `app.env` каждого сервиса — ничего копировать не нужно.

---

## Тестирование: пошаговая инструкция

### Шаг 1 — Проверка запуска

```bash
# Вариант А
curl -s http://localhost:8080/health
# => {"status":"ok"}

# Вариант Б
curl -s http://localhost:8080/health
# => {"status":"ok"}

# Вариант В — все 4 сервиса
curl -s http://localhost:8081/health  # auth
curl -s http://localhost:8082/health  # rooms
curl -s http://localhost:8083/health  # messages
curl -s http://localhost:8084/health  # notifications
```

---

### Шаг 2 — Регистрация и логин

Примеры для **Варианта А** (порт 8080).
Для **Б** — тот же порт 8080.
Для **В** — auth на порту 8081.

```bash
# Регистрация
curl -s -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username":"alice","password":"secret123"}' | jq .

# Ответ:
# { "token": "eyJ...", "user_id": "uuid", "username": "alice" }

# Сохранить токен
TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"alice","password":"secret123"}' | jq -r .token)

echo $TOKEN
```

---

### Шаг 3 — Комнаты

```bash
# Создать комнату
ROOM=$(curl -s -X POST http://localhost:8080/api/v1/rooms \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"general"}' | jq -r .room.id)

echo $ROOM

# Список комнат
curl -s http://localhost:8080/api/v1/rooms \
  -H "Authorization: Bearer $TOKEN" | jq .

# Второй пользователь вступает в комнату
TOKEN2=$(curl -s -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username":"bob","password":"secret123"}' | jq -r .token)

curl -s -X POST http://localhost:8080/api/v1/rooms/$ROOM/join \
  -H "Authorization: Bearer $TOKEN2"
# => {"message":"joined successfully"}
```

---

### Шаг 4 — WebSocket

Установить websocat:
```bash
# Linux
wget -O /usr/local/bin/websocat https://github.com/vi/websocat/releases/latest/download/websocat.x86_64-unknown-linux-musl
chmod +x /usr/local/bin/websocat

# macOS
brew install websocat
```

**Клиент 1** (alice) в одном терминале:
```bash
websocat "ws://localhost:8080/ws?token=$TOKEN"
# После подключения отправить:
{"type":"join","room_id":"ROOM_UUID_HERE"}
# Ответ: {"type":"joined","room_id":"...","user_id":"..."}
```

**Клиент 2** (bob) в другом терминале:
```bash
websocat "ws://localhost:8080/ws?token=$TOKEN2"
{"type":"join","room_id":"ROOM_UUID_HERE"}
# Отправить сообщение:
{"type":"message","room_id":"ROOM_UUID_HERE","content":"Привет, Alice!"}
```

Alice должна получить:
```json
{"type":"message","id":"...","room_id":"...","sender_id":"...","sender_username":"bob","content":"Привет, Alice!","created_at":"..."}
```

**Для Варианта В** WebSocket на порту 8084:
```bash
websocat "ws://localhost:8084/ws?token=$TOKEN"
```

---

### Шаг 5 — История сообщений

```bash
# Вариант А/Б
curl -s "http://localhost:8080/api/v1/rooms/$ROOM/messages?limit=20" \
  -H "Authorization: Bearer $TOKEN" | jq .

# Вариант В — через messages-service
curl -s "http://localhost:8083/api/v1/rooms/$ROOM/messages?limit=20" \
  -H "Authorization: Bearer $TOKEN" | jq .

# Пагинация — взять следующую страницу (before = id последнего сообщения)
LAST_ID=$(curl -s "http://localhost:8080/api/v1/rooms/$ROOM/messages?limit=5" \
  -H "Authorization: Bearer $TOKEN" | jq -r '.messages[-1].id')

curl -s "http://localhost:8080/api/v1/rooms/$ROOM/messages?before=$LAST_ID&limit=5" \
  -H "Authorization: Bearer $TOKEN" | jq .
```

---

## Нагрузочные тесты и метрики

### Установка k6

```bash
# macOS
brew install k6

# Linux (Ubuntu/Debian)
sudo gpg -k
sudo gpg --no-default-keyring --keyring /usr/share/keyrings/k6-archive-keyring.gpg \
  --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys C5AD17C747E3415A3642D57D77C6C491D6AC1D69
echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] https://dl.k6.io/deb stable main" \
  | sudo tee /etc/apt/sources.list.d/k6.list
sudo apt-get update && sudo apt-get install k6

# Или через Docker
alias k6='docker run --rm -i --network=host grafana/k6'
```

### Запуск тестов

```bash
cd load-test

# Вариант А (запустить вариант А первым)
k6 run --env BASE_URL=http://localhost:8080 scenario_a.js

# Вариант Б
k6 run --env BASE_URL=http://localhost:8080 scenario_b.js

# Вариант В
k6 run scenario_c.js
```

### Ключевые метрики и что они означают

```
✓ checks.........................: 99.80%
  http_req_duration..............: avg=45ms   min=2ms   med=38ms   max=1.2s   p(95)=180ms  p(99)=420ms
  message_latency_ms.............: avg=52ms   min=5ms   med=44ms   max=980ms  p(50)=44ms   p(95)=195ms  p(99)=450ms
  messages_sent..................: 48320
  messages_received..............: 47891
  http_req_failed................: 0.20%
```

| Метрика | Что показывает | Норма по ТЗ |
|---------|---------------|-------------|
| `message_latency_ms p(50)` | Медианная задержка от отправки до получения | < 100ms |
| `message_latency_ms p(95)` | 95-й перцентиль задержки | < 500ms |
| `message_latency_ms p(99)` | 99-й перцентиль задержки | < 1000ms |
| `http_req_failed` | Доля упавших HTTP-запросов | < 1% |
| `messages_sent` | Всего отправлено сообщений | — |
| `messages_received` | Всего получено через WS | — |

### Сохранить результаты для сравнения

```bash
# Сохранить результаты в JSON
k6 run --env BASE_URL=http://localhost:8080 scenario_a.js \
  --out json=results_a.json

k6 run --env BASE_URL=http://localhost:8080 scenario_b.js \
  --out json=results_b.json

k6 run scenario_c.js --out json=results_c.json

# Извлечь p95 latency для сравнения
for f in results_a.json results_b.json results_c.json; do
  echo "=== $f ==="
  cat $f | jq 'select(.type=="Point" and .metric=="message_latency_ms") | .data.value' \
    | awk '{s+=$1; n++} END {print "avg:", s/n, "ms"}'
done
```

### Сохранить в InfluxDB + Grafana (опционально)

```bash
# Запустить InfluxDB + Grafana
docker run -d -p 8086:8086 --name influxdb influxdb:1.8
docker run -d -p 3000:3000 --name grafana grafana/grafana

# Запустить тест с выводом в InfluxDB
k6 run --out influxdb=http://localhost:8086/k6 scenario_a.js
```

---

## Что проверить вручную

- [ ] `GET /health` возвращает `{"status":"ok"}` у всех сервисов
- [ ] Регистрация возвращает JWT-токен
- [ ] Нельзя зарегистрировать двух пользователей с одним username (409)
- [ ] Нельзя получить список комнат без токена (401)
- [ ] Вступление в уже вступленную комнату возвращает 409
- [ ] WebSocket подключается с токеном, отказывает без токена
- [ ] Сообщение от одного WS-клиента получает другой клиент в той же комнате
- [ ] История сообщений пагинируется через `?before=<id>&limit=N`
- [ ] Graceful shutdown: `docker-compose stop` корректно завершает все соединения
