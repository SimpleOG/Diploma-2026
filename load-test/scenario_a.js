// Сценарий нагрузочного теста для Варианта А (монолит, порт 8080)
// Запуск: k6 run --env BASE_URL=http://localhost:8080 scenario_a.js
//
// Как считается latency:
//   При отправке WS-сообщения клиент вшивает метку времени в content:
//     "Hello:ts:1711234567890"
//   Когда сообщение возвращается через broadcast (серверу broadcast включает отправителя),
//   клиент вычитает метку из текущего времени:
//     latency = Date.now() - sentAt
//   Это end-to-end RTT: отправка → сервер принял → сохранил в PostgreSQL → разослал обратно.

import ws from 'k6/ws';
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend, Counter } from 'k6/metrics';
import { formatSummary } from './summary.js';

// ── Кастомные метрики ────────────────────────────────────────────────────────
// Trend собирает перцентили (p50, p95, p99) автоматически
const messageLatency  = new Trend('message_latency_ms', true);
// Counter просто суммирует значения
const messagesSent    = new Counter('messages_sent');
const messagesReceived = new Counter('messages_received');

// ── Конфигурация теста ───────────────────────────────────────────────────────
export const options = {
  stages: [
    { duration: '1m',  target: 500  },  // разгон
    { duration: '2m',  target: 2000 },  // средняя нагрузка
    { duration: '3m',  target: 5000 },  // высокая нагрузка
    { duration: '2m',  target: 10000 }, // пик 10k VU
    { duration: '1m',  target: 0    },  // снижение
  ],
  thresholds: {
    message_latency_ms: ['p(50)<500', 'p(95)<3000', 'p(99)<10000'],
    http_req_failed:    ['rate<0.1'],
  },
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'],
};

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const WS_URL   = BASE_URL.replace('http://', 'ws://').replace('https://', 'wss://');

// ── setup() — выполняется один раз перед стартом VU ─────────────────────────
// ── handleSummary() — запускается после теста, пишет файлы ──────────────────
export function handleSummary(data) {
  const txt = formatSummary(data, 'Вариант А: Монолит + PostgreSQL + Redis (порт 8080)');
  return {
    'results_a.json': JSON.stringify(data, null, 2),
    'results_a.txt':  txt,
    stdout:           txt,
  };
}

export function setup() {
  const regRes = http.post(
    `${BASE_URL}/api/v1/auth/register`,
    JSON.stringify({ username: `setup_a_${Date.now()}_${Math.random().toString(36).slice(2, 8)}`, password: 'setuppass123' }),
    { headers: { 'Content-Type': 'application/json' } }
  );
  if (regRes.status !== 201) {
    console.error(`setup: register failed ${regRes.status}: ${regRes.body}`);
    return { roomId: null };
  }

  const token = regRes.json('token');
  const roomRes = http.post(
    `${BASE_URL}/api/v1/rooms`,
    JSON.stringify({ name: `load-test-a-${Date.now()}` }),
    { headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${token}` } }
  );

  // Вариант А возвращает плоский объект: {"id":"...","name":"...","owner_id":"..."}
  const roomId = roomRes.json('id');
  if (!roomId) {
    console.error(`setup: room creation failed ${roomRes.status}: ${roomRes.body}`);
    return { roomId: null };
  }
  console.log(`setup: shared room id=${roomId}`);
  return { roomId };
}

// ── Основной сценарий (выполняется каждым VU) ────────────────────────────────
export default function (data) {
  const username = `user_a_${__VU}_${__ITER}_${Math.random().toString(36).slice(2, 8)}`;

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

  // 2. Вступить в общую комнату (или создать свою если setup не сработал)
  let roomId = data.roomId;
  if (roomId) {
    const joinRes = http.post(
      `${BASE_URL}/api/v1/rooms/${roomId}/join`, null,
      { headers: { 'Authorization': `Bearer ${token}` } }
    );
    // 200 = успешно, 409 = уже состоит — оба ок
    check(joinRes, { 'join 200|409': (r) => r.status === 200 || r.status === 409 });
  } else {
    const roomRes = http.post(
      `${BASE_URL}/api/v1/rooms`,
      JSON.stringify({ name: `room_a_${__VU}_${Date.now()}` }),
      { headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${token}` } }
    );
    roomId = roomRes.json('id');
  }
  if (!roomId) return;

  // 3. WebSocket соединение
  const res = ws.connect(`${WS_URL}/ws?token=${token}`, {}, function (socket) {
    let msgCount = 0;
    const MAX_MSGS = 10;

    socket.on('open', function () {
      // После подключения отправляем join-сообщение
      socket.send(JSON.stringify({ type: 'join', room_id: roomId }));
    });

    socket.on('message', function (raw) {
      let msg;
      try { msg = JSON.parse(raw); } catch (e) { return; }

      // Сервер подтверждает вступление → начинаем отправку
      if (msg.type === 'joined') {
        let done = false;
        socket.setInterval(function () {
          // clearInterval недоступен в k6 — используем флаг
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

      // Принятое сообщение → вычисляем latency
      if (msg.type === 'message') {
        const content = msg.content || '';
        const parts = content.split(':ts:');
        if (parts.length === 2) {
          const sentAt = parseInt(parts[1], 10);
          const latency = Date.now() - sentAt;
          // Отбрасываем аномалии (отрицательные или > 60с — чужие старые сообщения)
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

    // Жёсткий таймаут на случай зависания
    socket.setTimeout(() => socket.close(), 35000);
  });

  check(res, { 'ws 101': (r) => r && r.status === 101 });
  sleep(1);
}
