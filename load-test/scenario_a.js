// Scenario for Variant A (port 8080)
// Run: k6 run --env BASE_URL=http://localhost:8080 scenario_a.js

import ws from 'k6/ws';
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend, Counter } from 'k6/metrics';
import { SharedArray } from 'k6/data';

const messageLatency = new Trend('message_latency_ms', true);
const messagesSent = new Counter('messages_sent');
const messagesReceived = new Counter('messages_received');

export const options = {
  stages: [
    { duration: '1m', target: 100 },
    { duration: '2m', target: 500 },
    { duration: '2m', target: 1000 },
    { duration: '1m', target: 0 },
  ],
  thresholds: {
    message_latency_ms: ['p(50)<100', 'p(95)<500', 'p(99)<1000'],
    http_req_failed: ['rate<0.01'],
  },
};

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const WS_URL = BASE_URL.replace('http://', 'ws://').replace('https://', 'wss://');

// Shared room ID created by VU 1
const rooms = new SharedArray('rooms', function() {
  return [{ id: null }];
});

export function setup() {
  // Create a test room as setup
  const regRes = http.post(`${BASE_URL}/api/v1/auth/register`, JSON.stringify({
    username: `setup_user_${Date.now()}`,
    password: 'setuppass123'
  }), { headers: { 'Content-Type': 'application/json' } });

  if (regRes.status !== 201) return { roomId: null };

  const token = regRes.json('token');
  const roomRes = http.post(`${BASE_URL}/api/v1/rooms`, JSON.stringify({
    name: `load-test-room-${Date.now()}`
  }), { headers: {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${token}`
  }});

  return { roomId: roomRes.json('id'), token };
}

export default function(data) {
  const username = `user_${__VU}_${Date.now()}`;

  // 1. Register
  const regRes = http.post(`${BASE_URL}/api/v1/auth/register`, JSON.stringify({
    username,
    password: 'testpass123'
  }), { headers: { 'Content-Type': 'application/json' } });

  check(regRes, {
    'register status 201': (r) => r.status === 201,
    'register has token': (r) => r.json('token') !== undefined,
  });

  if (regRes.status !== 201) return;

  const token = regRes.json('token');
  let roomId = data.roomId;

  // 2. Join or create room
  if (!roomId) {
    const roomRes = http.post(`${BASE_URL}/api/v1/rooms`, JSON.stringify({
      name: `room_${__VU}_${Date.now()}`
    }), { headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${token}`
    }});
    roomId = roomRes.json('id');
  } else {
    http.post(`${BASE_URL}/api/v1/rooms/${roomId}/join`, null, {
      headers: { 'Authorization': `Bearer ${token}` }
    });
  }

  if (!roomId) return;

  // 3. Connect WebSocket
  const wsUrl = `${WS_URL}/ws?token=${token}`;

  const res = ws.connect(wsUrl, {}, function(socket) {
    socket.on('open', function() {
      // Join room
      socket.send(JSON.stringify({ type: 'join', room_id: roomId }));
    });

    socket.on('message', function(data) {
      const msg = JSON.parse(data);

      if (msg.type === 'message' && msg.content) {
        // Try to parse timestamp from content
        try {
          const parts = msg.content.split(':ts:');
          if (parts.length === 2) {
            const sentAt = parseInt(parts[1]);
            const latency = Date.now() - sentAt;
            if (latency >= 0 && latency < 60000) {
              messageLatency.add(latency);
            }
            messagesReceived.add(1);
          }
        } catch(e) {}
      }

      if (msg.type === 'joined') {
        // Start sending messages every 2 seconds
        let msgCount = 0;
        const interval = socket.setInterval(function() {
          if (msgCount >= 10) {
            socket.clearInterval(interval);
            socket.close();
            return;
          }
          socket.send(JSON.stringify({
            type: 'message',
            room_id: roomId,
            content: `Hello from ${username}:ts:${Date.now()}`
          }));
          messagesSent.add(1);
          msgCount++;
        }, 2000);
      }

      if (msg.type === 'error') {
        console.log(`WS Error: ${msg.message}`);
      }
    });

    socket.on('error', function(e) {
      console.log(`WebSocket error: ${e.error()}`);
    });

    socket.setTimeout(function() {
      socket.close();
    }, 30000);
  });

  check(res, { 'ws connected': (r) => r && r.status === 101 });

  sleep(1);
}

export function teardown(data) {
  console.log('Load test completed');
}
