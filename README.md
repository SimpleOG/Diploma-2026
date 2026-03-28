# Master's Thesis: Instant Messaging System — 3 Architectural Variants

This repository contains a real-time chat application implemented as three
distinct architectural variants for comparative performance analysis.

## Table of Contents

- [Project Overview](#project-overview)
- [Architecture Comparison](#architecture-comparison)
- [Port Mapping](#port-mapping)
- [Tech Stack](#tech-stack)
- [Quick Start](#quick-start)
- [Manual Testing — curl](#manual-testing--curl)
- [Manual Testing — WebSocket (websocat)](#manual-testing--websocket-websocat)
- [Load Testing](#load-testing)
- [Performance Comparison Methodology](#performance-comparison-methodology)

---

## Project Overview

The thesis investigates how architectural decisions affect the throughput,
latency, and resource consumption of a real-time messaging system under
concurrent load.

Three variants are implemented:

| # | Name | Description |
|---|------|-------------|
| **A** | Monolith + PostgreSQL + Redis | Single deployable binary; WebSocket hub uses Redis pub/sub for horizontal scale |
| **B** | Monolith + Kafka + MongoDB + Memcached | Single binary; messages go through Kafka for async persistence; room membership cached in Memcached |
| **C** | Microservices + RabbitMQ | Four independent services; HTTP-only message submission; notifications delivered via dedicated WebSocket service |

---

## Architecture Comparison

| Aspect | Variant A | Variant B | Variant C |
|---|---|---|---|
| Deployment model | Single binary | Single binary | 4 services |
| HTTP API | Gin (monolith) | Gin (monolith) | Gin per service |
| WebSocket broker | Redis pub/sub | In-process hub | RabbitMQ topic exchange |
| Message persistence | PostgreSQL (sync) | MongoDB (async via Kafka) | PostgreSQL (sync, messages-svc) |
| Membership cache | Redis SET | Memcached | Redis SET (rooms-svc) |
| Message delivery | WS → persist → broadcast | WS → Kafka → worker → broadcast | HTTP POST → RabbitMQ → notify-svc → WS |
| Horizontal scaling | Via Redis pub/sub | Via Kafka consumer group | Independent service scaling |
| Number of databases | 1 (PG) + Redis | 1 (PG) + Mongo + Kafka + Memcached | 2 (PG×2) + Redis + RabbitMQ |

---

## Port Mapping

### Variant A

| Service | Host Port |
|---------|-----------|
| Chat server | **8080** |
| PostgreSQL | 5432 |
| Redis | 6379 |

### Variant B

| Service | Host Port |
|---------|-----------|
| Chat server | **8081** |
| PostgreSQL | 5433 |
| MongoDB | 27017 |
| Kafka | 9092 |
| Memcached | 11211 |
| Redis | 6380 |

### Variant C

| Service | Host Port |
|---------|-----------|
| auth-service | **8081** |
| rooms-service | **8082** |
| messages-service | **8083** |
| notifications-service | **8084** |
| auth PostgreSQL | 5434 |
| rooms PostgreSQL | 5435 |
| Redis (rooms) | 6381 |
| RabbitMQ AMQP | 5672 |
| RabbitMQ Management | 15672 |

---

## Tech Stack

### Variant A

| Layer | Technology |
|---|---|
| Language | Go 1.22 |
| HTTP framework | Gin |
| WebSocket library | gorilla/websocket |
| Auth | JWT (golang-jwt/jwt v5) |
| Primary DB | PostgreSQL 16 |
| Cache / pub-sub | Redis 7 |
| Migrations | golang-migrate |
| Container | Docker Compose |

### Variant B

| Layer | Technology |
|---|---|
| Language | Go 1.22 |
| HTTP framework | Gin |
| WebSocket library | gorilla/websocket |
| Auth | JWT (golang-jwt/jwt v5) |
| User / Room DB | PostgreSQL 16 |
| Message store | MongoDB 7 |
| Message queue | Apache Kafka 3 (confluent-kafka-go v2) |
| Membership cache | Memcached |
| Session / pub-sub | Redis 7 |
| Container | Docker Compose |

### Variant C

| Layer | Technology |
|---|---|
| Language | Go 1.22 (per service) |
| HTTP framework | Gin (per service) |
| WebSocket library | gorilla/websocket (notifications-svc) |
| Auth | JWT; internal `/internal/auth/validate` endpoint |
| Auth DB | PostgreSQL 16 |
| Rooms DB | PostgreSQL 16 |
| Message broker | RabbitMQ 3 (amqp091-go), topic exchange `messaging.events` |
| Rooms membership cache | Redis 7 |
| Container | Docker Compose |

---

## Quick Start

### Prerequisites

- Docker >= 24 and Docker Compose v2
- At least 4 GB of free RAM (Variant B needs the most due to Kafka + Mongo)

---

### Variant A

```bash
cd variant-a

# Create environment file
cat > .env <<'EOF'
SERVER_PORT=8080
DB_DSN=postgres://chatuser:chatpass@postgres:5432/chatdb?sslmode=disable
REDIS_ADDR=redis:6379
JWT_SECRET=change-me-in-production
JWT_EXPIRATION_HOURS=24
EOF

docker-compose up --build -d

# Check health
curl http://localhost:8080/health
```

Stop:

```bash
docker-compose down -v
```

---

### Variant B

```bash
cd variant-b

cat > .env <<'EOF'
SERVER_PORT=8081
DB_DSN=postgres://chatuser:chatpass@postgres:5433/chatdb?sslmode=disable
MONGO_URI=mongodb://mongo:27017
MONGO_DB=chatdb
KAFKA_BROKERS=kafka:9092
KAFKA_TOPIC=chat.messages
KAFKA_CONSUMER_GROUP=chat-workers
MEMCACHED_ADDR=memcached:11211
REDIS_ADDR=redis:6380
JWT_SECRET=change-me-in-production
JWT_EXPIRATION_HOURS=24
WORKER_INSTANCES=4
EOF

docker-compose up --build -d

curl http://localhost:8081/health
```

Stop:

```bash
docker-compose down -v
```

---

### Variant C

```bash
cd variant-c

# auth-service
cat > auth-service/.env <<'EOF'
SERVER_PORT=8081
DB_DSN=postgres://authuser:authpass@auth-postgres:5432/auth_db?sslmode=disable
JWT_SECRET=change-me-in-production
JWT_EXPIRATION_HOURS=24
EOF

# rooms-service
cat > rooms-service/.env <<'EOF'
SERVER_PORT=8082
DB_DSN=postgres://roomsuser:roomspass@rooms-postgres:5432/rooms_db?sslmode=disable
REDIS_ADDR=redis:6381
AUTH_SERVICE_URL=http://auth-service:8081
EOF

# messages-service
cat > messages-service/.env <<'EOF'
SERVER_PORT=8083
RABBITMQ_URL=amqp://guest:guest@rabbitmq:5672/
AUTH_SERVICE_URL=http://auth-service:8081
ROOMS_SERVICE_URL=http://rooms-service:8082
EOF

# notifications-service
cat > notifications-service/.env <<'EOF'
SERVER_PORT=8084
RABBITMQ_URL=amqp://guest:guest@rabbitmq:5672/
AUTH_SERVICE_URL=http://auth-service:8081
EOF

docker-compose up --build -d

# Health checks
curl http://localhost:8081/health
curl http://localhost:8082/health
curl http://localhost:8083/health
curl http://localhost:8084/health
```

Stop:

```bash
docker-compose down -v
```

---

## Manual Testing — curl

The examples below use Variant A (port 8080). Substitute `8081` for Variant B.
For Variant C, route each request to the appropriate service port.

### Register a user

```bash
curl -s -X POST http://localhost:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"secret123"}' | jq .
```

Expected response (HTTP 201):

```json
{
  "token": "<jwt>",
  "user_id": "<uuid>",
  "username": "alice"
}
```

### Login

```bash
curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"secret123"}' | jq .
```

### Store the token

```bash
TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"secret123"}' | jq -r '.token')
```

### List rooms

```bash
curl -s http://localhost:8080/api/v1/rooms \
  -H "Authorization: Bearer $TOKEN" | jq .
```

### Create a room

```bash
curl -s -X POST http://localhost:8080/api/v1/rooms \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"name":"general"}' | jq .
```

Expected response (HTTP 201):

```json
{
  "room": {
    "id": "<uuid>",
    "name": "general",
    "owner_id": "<uuid>",
    "member_count": 1,
    "created_at": "2026-03-28T12:00:00Z"
  }
}
```

### Join a room

```bash
ROOM_ID="<uuid-from-above>"

curl -s -X POST "http://localhost:8080/api/v1/rooms/${ROOM_ID}/join" \
  -H "Authorization: Bearer $TOKEN" | jq .
```

### Get room message history

```bash
curl -s "http://localhost:8080/api/v1/rooms/${ROOM_ID}/messages?limit=20" \
  -H "Authorization: Bearer $TOKEN" | jq .
```

---

### Variant C — microservice-specific curl examples

Register via auth-service:

```bash
TOKEN=$(curl -s -X POST http://localhost:8081/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"bob","password":"secret123"}' | jq -r '.token')
```

Create room via rooms-service:

```bash
ROOM_ID=$(curl -s -X POST http://localhost:8082/api/v1/rooms \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"name":"lobby"}' | jq -r '.room.id')
```

Send message via messages-service:

```bash
curl -s -X POST "http://localhost:8083/api/v1/rooms/${ROOM_ID}/messages" \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"content":"Hello from microservices!"}' | jq .
```

---

## Manual Testing — WebSocket (websocat)

### Install websocat

```bash
# Linux (x86_64)
curl -Lo websocat \
  https://github.com/vi/websocat/releases/latest/download/websocat.x86_64-unknown-linux-musl
chmod +x websocat
sudo mv websocat /usr/local/bin/

# macOS
brew install websocat
```

### Connect — Variants A and B

The WebSocket endpoint accepts the JWT as a query parameter:

```bash
TOKEN="<your-jwt>"
ROOM_ID="<your-room-uuid>"

websocat "ws://localhost:8080/ws?token=${TOKEN}"
```

After connecting, join a room by sending JSON:

```bash
# In the websocat session, type:
{"type":"join","room_id":"<ROOM_ID>"}
```

Once joined, send a message:

```bash
{"type":"message","room_id":"<ROOM_ID>","content":"Hello world!"}
```

### Connect — Variant A (full one-liner)

```bash
TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"secret123"}' | jq -r '.token')

websocat "ws://localhost:8080/ws?token=${TOKEN}"
```

### Connect — Variant B

```bash
# Variant B WS requires room_id as a query parameter (not a join message)
TOKEN=$(curl -s -X POST http://localhost:8081/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"secret123"}' | jq -r '.token')

ROOM_ID="<uuid>"

websocat "ws://localhost:8081/ws?token=${TOKEN}&room_id=${ROOM_ID}"

# Then send messages:
# {"type":"message","content":"Hello!"}
```

### Connect — Variant C (notifications-service)

```bash
TOKEN=$(curl -s -X POST http://localhost:8081/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"secret123"}' | jq -r '.token')

ROOM_ID="<uuid>"

# Connect to notifications-service for inbound push
websocat "ws://localhost:8084/ws?token=${TOKEN}&room_id=${ROOM_ID}"

# In another terminal, post a message and watch it arrive on the WS:
curl -s -X POST "http://localhost:8083/api/v1/rooms/${ROOM_ID}/messages" \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"content":"Delivered via RabbitMQ!"}' | jq .
```

### Two-client chat example (Variant A)

Terminal 1 — Alice:

```bash
T1=$(curl -s -X POST http://localhost:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice_ws","password":"pass1234"}' | jq -r '.token')

ROOM=$(curl -s -X POST http://localhost:8080/api/v1/rooms \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $T1" \
  -d '{"name":"test-room"}' | jq -r '.room.id')

echo "Room: $ROOM"
websocat "ws://localhost:8080/ws?token=${T1}"
# then type: {"type":"join","room_id":"<ROOM>"}
```

Terminal 2 — Bob:

```bash
T2=$(curl -s -X POST http://localhost:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"bob_ws","password":"pass1234"}' | jq -r '.token')

curl -s -X POST "http://localhost:8080/api/v1/rooms/<ROOM>/join" \
  -H "Authorization: Bearer $T2"

websocat "ws://localhost:8080/ws?token=${T2}"
# then type: {"type":"join","room_id":"<ROOM>"}
# then type: {"type":"message","room_id":"<ROOM>","content":"Hi Alice!"}
```

---

## Load Testing

The `load-test/` directory contains k6 scenarios for all three variants.

### Run Variant A scenario

```bash
# Install k6 first (see load-test/README.md for platform-specific instructions)

cd load-test
k6 run scenario_a.js
```

### Run Variant B scenario

```bash
k6 run scenario_b.js
```

### Run Variant C scenario

```bash
k6 run scenario_c.js
```

### Load profile used in all scenarios

| Stage | Duration | Target VUs |
|---|---|---|
| Ramp-up | 1 min | 100 |
| Mid load | 2 min | 500 |
| Peak load | 2 min | 1000 |
| Ramp-down | 1 min | 0 |

### Thresholds (pass / fail criteria)

| Metric | Variants A / B | Variant C |
|---|---|---|
| `message_latency_ms` p(50) | < 100 ms | < 200 ms |
| `message_latency_ms` p(95) | < 500 ms | < 800 ms |
| `message_latency_ms` p(99) | < 1000 ms | < 2000 ms |
| `http_req_failed` | < 1 % | < 1 % |

For a full description of all metrics and a comparison methodology, see
[load-test/README.md](load-test/README.md).

---

## Performance Comparison Methodology

The thesis compares the three variants across the following dimensions:

### 1. Controlled environment

- All variants run on the same host with Docker resource limits enforced:
  - Server containers: 2 CPU cores, 512 MB RAM
  - Database containers: 2 CPU cores, 1 GB RAM
- The k6 load generator runs on a separate machine to avoid competing for CPU.

### 2. Metrics collected

| Category | Metric | Source |
|---|---|---|
| Latency | `message_latency_ms` p50 / p95 / p99 | k6 custom trend |
| Throughput | Messages / second at peak | k6 counter / duration |
| Error rate | `http_req_failed` rate | k6 built-in |
| Connection success | `ws connected` check pass rate | k6 check |
| HTTP sub-latency (C only) | `auth_latency_ms`, `rooms_latency_ms`, `post_message_latency_ms` | k6 custom trends |
| CPU | % utilisation per container | `docker stats` |
| Memory | RSS per container | `docker stats` |
| Network I/O | Bytes in/out per container | `docker stats` |

### 3. Test protocol

1. Cold-start all containers (`docker-compose up -d`).
2. Wait for health-check endpoints to return HTTP 200.
3. Run the warm-up phase: 50 VUs × 2 min (results discarded).
4. Run the full k6 scenario and save JSON output:
   ```bash
   k6 run --out json=results_<variant>.json scenario_<variant>.js
   ```
5. Collect container-level metrics at 10-second intervals during the test:
   ```bash
   docker stats --no-stream --format \
     "{{.Name}},{{.CPUPerc}},{{.MemUsage}},{{.NetIO}}" \
     >> docker_stats_<variant>.csv
   ```
6. Stop all containers and repeat steps 1–5 three times per variant.
7. Average the three runs before drawing conclusions.

### 4. Comparison focus

- **Variant A vs B** — Effect of introducing Kafka for async persistence and
  MongoDB as the message store. Hypothesis: lower write latency at peak load
  because WS handler returns after enqueuing, not after DB commit.
- **Variant B vs C** — Effect of decomposing into microservices. Hypothesis:
  higher tail latency due to extra network hops, but better independent
  scalability per bottleneck.
- **Variant A vs C** — Monolith vs full microservices baseline comparison.

### 5. Reproducibility

All Docker Compose files, k6 scripts, and environment templates are checked
into this repository. To reproduce a run on a fresh machine:

```bash
git clone <repo>
cd Diploma-2026
# Pick a variant, follow the Quick Start section above.
# Then run the corresponding k6 scenario.
```
