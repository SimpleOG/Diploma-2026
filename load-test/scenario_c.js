// Сценарий нагрузочного теста для Варианта С (микросервисы)
// Запуск: k6 run scenario_c.js
// Переопределение URL отдельных сервисов:
//   k6 run \
//     --env AUTH_URL=http://localhost:8081 \
//     --env ROOMS_URL=http://localhost:8082 \
//     --env MESSAGES_URL=http://localhost:8083 \
//     --env WS_URL=ws://localhost:8084 \
//     scenario_c.js
//
// Как считается latency:
//   Клиент вшивает метку времени в content: "ping:ts:1711234567890"
//   notifications-service сохраняет content без изменений и отдаёт через WS
//   Когда сообщение приходит (type="new_message"), клиент вычитает метку:
//     latency = Date.now() - sentAt
//   Это end-to-end RTT: POST /messages → MongoDB → RabbitMQ → notifications → WS клиент.

import ws from 'k6/ws';
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend, Counter } from 'k6/metrics';

// ── Кастомные метрики ────────────────────────────────────────────────────────
const messageLatency    = new Trend('message_latency_ms', true);
const messagesSent      = new Counter('messages_sent');
const messagesReceived  = new Counter('messages_received');

// Latency отдельных сервисов
const authLatency       = new Trend('auth_latency_ms', true);
const roomsLatency      = new Trend('rooms_latency_ms', true);
const postMsgLatency    = new Trend('post_message_latency_ms', true);

// ── Конфигурация теста ───────────────────────────────────────────────────────
const AUTH_URL     = __ENV.AUTH_URL     || 'http://localhost:8081';
const ROOMS_URL    = __ENV.ROOMS_URL    || 'http://localhost:8082';
const MESSAGES_URL = __ENV.MESSAGES_URL || 'http://localhost:8083';
const WS_URL       = __ENV.WS_URL       || 'ws://localhost:8084';

export const options = {
  stages: [
    { duration: '1m', target: 100  },  // разгон до 100 VU
    { duration: '2m', target: 500  },  // рост до 500
    { duration: '2m', target: 1000 },  // пиковая нагрузка 1000 VU
    { duration: '1m', target: 0    },  // снижение
  ],
  thresholds: {
    // End-to-end: POST /messages → RabbitMQ → WS push
    message_latency_ms:       ['p(50)<200', 'p(95)<800', 'p(99)<2000'],
    auth_latency_ms:          ['p(95)<300'],
    rooms_latency_ms:         ['p(95)<300'],
    post_message_latency_ms:  ['p(95)<500'],
    http_req_failed:          ['rate<0.01'],
  },
};

// ── setup() — выполняется один раз перед стартом VU ─────────────────────────
export function setup() {
  const regRes = http.post(
    `${AUTH_URL}/api/v1/auth/register`,
    JSON.stringify({ username: `setup_c_${Date.now()}_${Math.random().toString(36).slice(2, 8)}`, password: 'setuppass123' }),
    { headers: { 'Content-Type': 'application/json' } }
  );
  if (regRes.status !== 201) {
    console.error(`setup: register failed ${regRes.status}: ${regRes.body}`);
    return { roomId: null };
  }

  const token = regRes.json('token');

  const roomRes = http.post(
    `${ROOMS_URL}/api/v1/rooms`,
    JSON.stringify({ name: `load-test-c-${Date.now()}` }),
    { headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${token}` } }
  );

  // Rooms-service возвращает плоский объект: {"id":"...","name":"...","owner_id":"..."}
  const roomId = roomRes.json('id');
  if (!roomId) {
    console.error(`setup: room creation failed ${roomRes.status}: ${roomRes.body}`);
    return { roomId: null };
  }
  console.log(`setup: shared room id=${roomId}`);
  return { roomId };
}

// ── Основной сценарий (выполняется каждым VU) ────────────────────────────────
export default function(data) {
  const username = `user_c_${__VU}_${__ITER}_${Math.random().toString(36).slice(2, 8)}`;

  // 1. Регистрация через auth-service
  const regStart = Date.now();
  const regRes = http.post(
    `${AUTH_URL}/api/v1/auth/register`,
    JSON.stringify({ username, password: 'testpass123' }),
    { headers: { 'Content-Type': 'application/json' } }
  );
  authLatency.add(Date.now() - regStart);

  check(regRes, {
    'register 201':   (r) => r.status === 201,
    'register token': (r) => !!r.json('token'),
  });
  if (regRes.status !== 201) return;
  const token = regRes.json('token');

  // 2. Вступить в общую комнату (или создать свою если setup не сработал)
  let roomId = data.roomId;
  if (roomId) {
    const joinStart = Date.now();
    const joinRes = http.post(
      `${ROOMS_URL}/api/v1/rooms/${roomId}/join`, null,
      { headers: { 'Authorization': `Bearer ${token}` } }
    );
    roomsLatency.add(Date.now() - joinStart);
    // 200 = успешно, 409 = уже состоит — оба ок
    check(joinRes, { 'join 200|409': (r) => r.status === 200 || r.status === 409 });
  } else {
    const createStart = Date.now();
    const roomRes = http.post(
      `${ROOMS_URL}/api/v1/rooms`,
      JSON.stringify({ name: `room_c_${__VU}_${Date.now()}` }),
      { headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${token}` } }
    );
    roomsLatency.add(Date.now() - createStart);
    roomId = roomRes.json('id');
  }
  if (!roomId) return;

  // 3. WebSocket соединение к notifications-service
  // Токен передаётся как query-параметр (браузер не может задать заголовки при WS)
  const res = ws.connect(`${WS_URL}/ws?token=${token}`, {}, function(socket) {
    let sendInterval = null;
    let msgCount = 0;
    const MAX_MSGS = 10;

    socket.on('open', function() {
      // После подключения отправляем join-сообщение (аналогично Вариантам А)
      socket.send(JSON.stringify({ type: 'join', room_id: roomId }));
    });

    socket.on('message', function(raw) {
      let msg;
      try { msg = JSON.parse(raw); } catch (e) { return; }

      // Сервер подтверждает вступление → начинаем отправку сообщений
      if (msg.type === 'joined') {
        sendInterval = socket.setInterval(function() {
          if (msgCount >= MAX_MSGS) {
            socket.clearInterval(sendInterval);
            socket.setTimeout(() => socket.close(), 5000);
            return;
          }

          const sentAt = Date.now();
          const content = `ping:ts:${sentAt}`;

          // Сообщения отправляются через messages-service REST API
          // messages-service сохраняет в MongoDB, публикует в RabbitMQ,
          // notifications-service получает из очереди и рассылает через WS
          const postStart = Date.now();
          const msgRes = http.post(
            `${MESSAGES_URL}/api/v1/messages`,
            JSON.stringify({ room_id: roomId, content }),
            {
              headers: {
                'Content-Type': 'application/json',
                'Authorization': `Bearer ${token}`,
              },
            }
          );
          postMsgLatency.add(Date.now() - postStart);

          check(msgRes, { 'messages: post 201': (r) => r.status === 201 });

          if (msgRes.status === 201) {
            messagesSent.add(1);
          }
          msgCount++;
        }, 2000);
        return;
      }

      // Notifications-service рассылает события типа "new_message"
      if (msg.type === 'new_message') {
        const content = msg.content || '';
        const parts = content.split(':ts:');
        if (parts.length === 2) {
          const sentAt = parseInt(parts[1], 10);
          const latency = Date.now() - sentAt;
          // Отбрасываем аномалии (отрицательные или > 120с)
          if (latency >= 0 && latency < 120000) {
            messageLatency.add(latency);
          }
          messagesReceived.add(1);
        }
      }

      if (msg.type === 'error') {
        console.log(`VU${__VU} WS error: ${msg.message}`);
      }
    });

    socket.on('error', (e) => console.log(`VU${__VU} WS error: ${e.error()}`));

    // Жёсткий таймаут на случай зависания
    socket.setTimeout(() => socket.close(), 40000);
  });

  check(res, { 'ws 101': (r) => r && r.status === 101 });
  sleep(1);
}
