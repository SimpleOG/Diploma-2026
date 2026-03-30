// Сценарий нагрузочного теста для Варианта Б (монолит + Kafka, порт 8081)
// Запуск: k6 run --env BASE_URL=http://localhost:8081 scenario_b.js
//
// Особенности Варианта Б:
//   - WS-эндпоинт требует room_id в URL (?token=...&room_id=...)
//   - Сообщение сразу бродкастится оптимистично (до сохранения в MongoDB)
//   - latency = время от отправки до оптимистичного broadcast (очень быстро)
//   - Реальное сохранение происходит асинхронно: Kafka → Worker → MongoDB

import ws from 'k6/ws';
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend, Counter } from 'k6/metrics';

// ── Кастомные метрики ────────────────────────────────────────────────────────
const messageLatency   = new Trend('message_latency_ms', true);
const messagesSent     = new Counter('messages_sent');
const messagesReceived = new Counter('messages_received');

// ── Конфигурация ─────────────────────────────────────────────────────────────
export const options = {
  stages: [
    { duration: '30s', target: 20  },
    { duration: '1m',  target: 50  },
    { duration: '1m',  target: 100 },
    { duration: '30s', target: 0   },
  ],
  thresholds: {
    message_latency_ms: ['p(50)<200', 'p(95)<1000', 'p(99)<2000'],
    http_req_failed:    ['rate<0.05'],
  },
};

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8081';
const WS_URL   = BASE_URL.replace('http://', 'ws://').replace('https://', 'wss://');

// ── setup() ──────────────────────────────────────────────────────────────────
export function setup() {
  const regRes = http.post(
    `${BASE_URL}/api/v1/auth/register`,
    JSON.stringify({ username: `setup_b_${Date.now()}_${Math.random().toString(36).slice(2, 8)}`, password: 'setuppass123' }),
    { headers: { 'Content-Type': 'application/json' } }
  );
  if (regRes.status !== 201) {
    console.error(`setup: register failed ${regRes.status}: ${regRes.body}`);
    return { roomId: null };
  }

  const token = regRes.json('token');
  const roomRes = http.post(
    `${BASE_URL}/api/v1/rooms`,
    JSON.stringify({ name: `load-test-b-${Date.now()}` }),
    { headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${token}` } }
  );

  const roomId = roomRes.json('id');
  if (!roomId) {
    console.error(`setup: room creation failed ${roomRes.status}: ${roomRes.body}`);
    return { roomId: null };
  }
  console.log(`setup: shared room id=${roomId}`);
  return { roomId };
}

// ── Сценарий ─────────────────────────────────────────────────────────────────
export default function (data) {
  const username = `user_b_${__VU}_${__ITER}_${Math.random().toString(36).slice(2, 8)}`;

  // 1. Регистрация
  const regRes = http.post(
    `${BASE_URL}/api/v1/auth/register`,
    JSON.stringify({ username, password: 'testpass123' }),
    { headers: { 'Content-Type': 'application/json' } }
  );
  check(regRes, {
    'register 201':   (r) => r.status === 201,
    'register token': (r) => r.status === 201 && !!r.json('token'),
  });
  if (regRes.status !== 201 || !regRes.body) return;
  const token = regRes.json('token');
  if (!token) return;

  // 2. Вступить в комнату
  let roomId = data.roomId;
  if (roomId) {
    const joinRes = http.post(
      `${BASE_URL}/api/v1/rooms/${roomId}/join`, null,
      { headers: { 'Authorization': `Bearer ${token}` } }
    );
    check(joinRes, { 'join 200|409': (r) => r.status === 200 || r.status === 409 });
  } else {
    const roomRes = http.post(
      `${BASE_URL}/api/v1/rooms`,
      JSON.stringify({ name: `room_b_${__VU}_${Date.now()}` }),
      { headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${token}` } }
    );
    roomId = roomRes.json('id');
  }
  if (!roomId) return;

  // 3. WebSocket — Вариант Б требует room_id в URL при подключении
  const wsUrl = `${WS_URL}/ws?token=${token}&room_id=${roomId}`;

  const res = ws.connect(wsUrl, {}, function (socket) {
    let msgCount = 0;
    const MAX_MSGS = 10;

    socket.on('message', function (raw) {
      let msg;
      try { msg = JSON.parse(raw); } catch (e) { return; }

      // Сервер отправляет "joined" сразу после подключения → начинаем слать
      if (msg.type === 'joined') {
        let done = false;
        socket.setInterval(function () {
          if (done) return;
          if (msgCount >= MAX_MSGS) {
            done = true;
            socket.setTimeout(() => socket.close(), 3000);
            return;
          }
          const sentAt = Date.now();
          socket.send(JSON.stringify({
            type:    'message',
            room_id: roomId,
            content: `ping:ts:${sentAt}`,
          }));
          messagesSent.add(1);
          msgCount++;
        }, 2000);
        return;
      }

      // Принятое сообщение → замеряем latency
      if (msg.type === 'message') {
        const content = msg.content || '';
        const parts = content.split(':ts:');
        if (parts.length === 2) {
          const sentAt = parseInt(parts[1], 10);
          const latency = Date.now() - sentAt;
          if (latency >= 0 && latency < 60000) {
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
    socket.setTimeout(() => socket.close(), 35000);
  });

  check(res, { 'ws 101': (r) => r && r.status === 101 });
  sleep(1);
}
