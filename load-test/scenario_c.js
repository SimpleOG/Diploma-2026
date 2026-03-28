// Scenario for Variant C (microservices, port split)
// Auth:          http://localhost:8081
// Rooms:         http://localhost:8082
// Messages:      http://localhost:8083
// Notifications: ws://localhost:8084
//
// Run:
//   k6 run scenario_c.js
// Override individual service URLs:
//   k6 run \
//     --env AUTH_URL=http://localhost:8081 \
//     --env ROOMS_URL=http://localhost:8082 \
//     --env MESSAGES_URL=http://localhost:8083 \
//     --env WS_URL=ws://localhost:8084 \
//     scenario_c.js

import ws from 'k6/ws';
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend, Counter } from 'k6/metrics';

// ---------------------------------------------------------------------------
// Custom metrics
// ---------------------------------------------------------------------------

// End-to-end latency: time from HTTP POST /messages to receipt on WebSocket.
const messageLatency = new Trend('message_latency_ms', true);
const messagesSent    = new Counter('messages_sent');
const messagesReceived = new Counter('messages_received');

// Per-service HTTP latency trends.
const authLatency     = new Trend('auth_latency_ms', true);
const roomsLatency    = new Trend('rooms_latency_ms', true);
const postMsgLatency  = new Trend('post_message_latency_ms', true);

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

const AUTH_URL     = __ENV.AUTH_URL     || 'http://localhost:8081';
const ROOMS_URL    = __ENV.ROOMS_URL    || 'http://localhost:8082';
const MESSAGES_URL = __ENV.MESSAGES_URL || 'http://localhost:8083';
const WS_URL       = __ENV.WS_URL       || 'ws://localhost:8084';

// ---------------------------------------------------------------------------
// Load profile
// ---------------------------------------------------------------------------

export const options = {
  stages: [
    { duration: '1m', target: 100  },  // ramp-up
    { duration: '2m', target: 500  },  // ramp to mid
    { duration: '2m', target: 1000 },  // peak load
    { duration: '1m', target: 0    },  // ramp-down
  ],
  thresholds: {
    // End-to-end message latency (HTTP POST -> WS delivery).
    message_latency_ms:    ['p(50)<200', 'p(95)<800', 'p(99)<2000'],
    // Individual service HTTP latencies.
    auth_latency_ms:       ['p(95)<300'],
    rooms_latency_ms:      ['p(95)<300'],
    post_message_latency_ms: ['p(95)<500'],
    // Overall HTTP error rate.
    http_req_failed:       ['rate<0.01'],
  },
};

// ---------------------------------------------------------------------------
// setup() — runs once before VUs start; creates a shared room
// ---------------------------------------------------------------------------

export function setup() {
  // Register a dedicated setup user.
  const regRes = http.post(
    `${AUTH_URL}/api/v1/auth/register`,
    JSON.stringify({
      username: `setup_c_${Date.now()}`,
      password: 'setuppass123',
    }),
    { headers: { 'Content-Type': 'application/json' } }
  );

  if (regRes.status !== 201) {
    console.error(`setup: register failed – status ${regRes.status} body: ${regRes.body}`);
    return { roomId: null };
  }

  const token = regRes.json('token');

  // Create a room for all VUs to share.
  const roomRes = http.post(
    `${ROOMS_URL}/api/v1/rooms`,
    JSON.stringify({ name: `load-test-room-c-${Date.now()}` }),
    {
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${token}`,
      },
    }
  );

  const roomId = roomRes.json('room.id') || roomRes.json('id');

  if (!roomId) {
    console.error(`setup: room creation failed – status ${roomRes.status} body: ${roomRes.body}`);
    return { roomId: null };
  }

  console.log(`setup: shared room created – id=${roomId}`);
  return { roomId, setupToken: token };
}

// ---------------------------------------------------------------------------
// default function — per-VU scenario
// ---------------------------------------------------------------------------

export default function(data) {
  const vuTag = `${__VU}_${Date.now()}`;
  const username = `user_c_${vuTag}`;

  // ── 1. Register via auth-service ─────────────────────────────────────────
  const regStart = Date.now();
  const regRes = http.post(
    `${AUTH_URL}/api/v1/auth/register`,
    JSON.stringify({ username, password: 'testpass123' }),
    { headers: { 'Content-Type': 'application/json' } }
  );
  authLatency.add(Date.now() - regStart);

  check(regRes, {
    'auth: register 201':    (r) => r.status === 201,
    'auth: has token':       (r) => r.json('token') !== undefined,
  });

  if (regRes.status !== 201) {
    console.log(`VU ${__VU}: register failed (${regRes.status})`);
    return;
  }

  const token = regRes.json('token');

  // ── 2. Join (or create) the shared room via rooms-service ────────────────
  let roomId = data.roomId;

  if (roomId) {
    const joinStart = Date.now();
    const joinRes = http.post(
      `${ROOMS_URL}/api/v1/rooms/${roomId}/join`,
      null,
      { headers: { 'Authorization': `Bearer ${token}` } }
    );
    roomsLatency.add(Date.now() - joinStart);

    check(joinRes, {
      'rooms: join 200 or 409': (r) => r.status === 200 || r.status === 409,
    });
  } else {
    // No shared room from setup – create a private one.
    const createStart = Date.now();
    const roomRes = http.post(
      `${ROOMS_URL}/api/v1/rooms`,
      JSON.stringify({ name: `room_c_${vuTag}` }),
      {
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${token}`,
        },
      }
    );
    roomsLatency.add(Date.now() - createStart);

    check(roomRes, { 'rooms: create 201': (r) => r.status === 201 });

    roomId = roomRes.json('room.id') || roomRes.json('id');
  }

  if (!roomId) {
    console.log(`VU ${__VU}: no room ID available, skipping`);
    return;
  }

  // ── 3. Open WebSocket to notifications-service ───────────────────────────
  // The notifications-service listens for RabbitMQ events and pushes them to
  // connected WebSocket clients.  The token is passed as a query parameter.
  const wsEndpoint = `${WS_URL}/ws?token=${token}&room_id=${roomId}`;

  // A small map to track in-flight messages: msgId -> sentAt timestamp.
  const inflightMessages = {};
  let msgCount = 0;
  const MAX_MESSAGES = 10;
  let wsConnected = false;

  const res = ws.connect(wsEndpoint, {}, function(socket) {
    socket.on('open', function() {
      wsConnected = true;

      // ── 4. Send messages via messages-service REST API (not WS) ──────────
      // The messages-service receives the POST, persists to DB, and publishes
      // an event to RabbitMQ. The notifications-service consumes the event and
      // pushes it to all WS clients subscribed to that room.
      const interval = socket.setInterval(function() {
        if (msgCount >= MAX_MESSAGES) {
          socket.clearInterval(interval);
          // Give time for in-flight messages to arrive before closing.
          socket.setTimeout(function() {
            socket.close();
          }, 5000);
          return;
        }

        const sentAt = Date.now();
        const content = `Hello from ${username}:ts:${sentAt}`;

        const postStart = Date.now();
        const msgRes = http.post(
          `${MESSAGES_URL}/api/v1/rooms/${roomId}/messages`,
          JSON.stringify({ content }),
          {
            headers: {
              'Content-Type': 'application/json',
              'Authorization': `Bearer ${token}`,
            },
          }
        );
        postMsgLatency.add(Date.now() - postStart);

        check(msgRes, {
          'messages: post 201': (r) => r.status === 201,
        });

        if (msgRes.status === 201) {
          const msgId = msgRes.json('id') || msgRes.json('message_id') || `${__VU}_${msgCount}`;
          inflightMessages[msgId] = sentAt;
          messagesSent.add(1);
        }

        msgCount++;
      }, 2000);
    });

    socket.on('message', function(rawData) {
      let msg;
      try {
        msg = JSON.parse(rawData);
      } catch (e) {
        return;
      }

      // Notifications-service pushes an event with type "message" or "new_message".
      if (msg.type === 'message' || msg.type === 'new_message') {
        const content = msg.content || msg.body || '';

        // Primary latency path: timestamp embedded in content.
        const parts = content.split(':ts:');
        if (parts.length === 2) {
          const sentAt = parseInt(parts[1]);
          const latency = Date.now() - sentAt;
          if (latency >= 0 && latency < 120000) {
            messageLatency.add(latency);
          }
          messagesReceived.add(1);
          return;
        }

        // Fallback: use message_id to look up the send timestamp.
        const msgId = msg.id || msg.message_id;
        if (msgId && inflightMessages[msgId] !== undefined) {
          const latency = Date.now() - inflightMessages[msgId];
          if (latency >= 0 && latency < 120000) {
            messageLatency.add(latency);
          }
          delete inflightMessages[msgId];
          messagesReceived.add(1);
        }
      }

      if (msg.type === 'error') {
        console.log(`VU ${__VU} WS error: ${msg.message || rawData}`);
      }
    });

    socket.on('error', function(e) {
      console.log(`VU ${__VU} WebSocket error: ${e.error()}`);
    });

    // Hard timeout: close after 35 s regardless.
    socket.setTimeout(function() {
      socket.close();
    }, 35000);
  });

  check(res, { 'ws: connected 101': (r) => r && r.status === 101 });

  if (!wsConnected) {
    console.log(`VU ${__VU}: WebSocket connection failed`);
  }

  sleep(1);
}

// ---------------------------------------------------------------------------
// teardown() — runs once after all VUs finish
// ---------------------------------------------------------------------------

export function teardown(data) {
  console.log('Variant C load test completed.');
}
