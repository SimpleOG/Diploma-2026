# Load Test Scenarios

k6 load tests for all three architectural variants of the chat diploma project.

## Table of Contents

- [Installation](#installation)
- [Running the Scenarios](#running-the-scenarios)
- [Expected Output](#expected-output)
- [Metric Descriptions](#metric-descriptions)
- [Interpretation Guide](#interpretation-guide)
- [Comparing Variants](#comparing-variants)
- [Troubleshooting](#troubleshooting)

---

## Installation

### Linux (Debian / Ubuntu)

```bash
sudo gpg -k
sudo gpg --no-default-keyring \
  --keyring /usr/share/keyrings/k6-archive-keyring.gpg \
  --keyserver hkp://keyserver.ubuntu.com:80 \
  --recv-keys C5AD17C747E3415A3642D57D77C6C491D6AC1D69

echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] \
  https://dl.k6.io/deb stable main" \
  | sudo tee /etc/apt/sources.list.d/k6.list

sudo apt-get update
sudo apt-get install k6
```

### Linux (Fedora / RHEL / CentOS)

```bash
sudo dnf install https://dl.k6.io/rpm/repo.rpm
sudo dnf install k6
```

### macOS (Homebrew)

```bash
brew install k6
```

### Docker

```bash
docker pull grafana/k6
```

Running a test with Docker:

```bash
docker run --rm -i \
  --network host \
  -v "$(pwd)":/scripts \
  grafana/k6 run /scripts/scenario_a.js
```

### Verify installation

```bash
k6 version
```

---

## Running the Scenarios

### Prerequisites

Start the target variant before running a scenario. See the root `README.md`
for `docker-compose` quick-start commands for each variant.

### Scenario A — Variant A (monolith, port 8080)

```bash
cd load-test

# Default base URL is http://localhost:8080
k6 run scenario_a.js

# Override base URL
k6 run --env BASE_URL=http://192.168.1.10:8080 scenario_a.js
```

### Scenario B — Variant B (monolith + Kafka + Mongo, port 8081)

```bash
cd load-test

# Default base URL is http://localhost:8081
k6 run scenario_b.js

# Override base URL
k6 run --env BASE_URL=http://192.168.1.10:8081 scenario_b.js
```

### Scenario C — Variant C (microservices, four separate ports)

| Service              | Default URL                    | Override env var    |
|----------------------|--------------------------------|---------------------|
| auth-service         | `http://localhost:8081`        | `AUTH_URL`          |
| rooms-service        | `http://localhost:8082`        | `ROOMS_URL`         |
| messages-service     | `http://localhost:8083`        | `MESSAGES_URL`      |
| notifications-service| `ws://localhost:8084`          | `WS_URL`            |

```bash
cd load-test

# Defaults
k6 run scenario_c.js

# Custom URLs
k6 run \
  --env AUTH_URL=http://localhost:8081 \
  --env ROOMS_URL=http://localhost:8082 \
  --env MESSAGES_URL=http://localhost:8083 \
  --env WS_URL=ws://localhost:8084 \
  scenario_c.js
```

### Saving results to JSON for comparison

```bash
k6 run --out json=results_a.json scenario_a.js
k6 run --out json=results_b.json scenario_b.js
k6 run --out json=results_c.json scenario_c.js
```

### Exporting to InfluxDB + Grafana

```bash
k6 run --out influxdb=http://localhost:8086/k6 scenario_a.js
```

---

## Expected Output

A typical run summary looks like this (values are illustrative):

```
          /\      |‾‾| /‾‾/   /‾‾/
     /\  /  \     |  |/  /   /  /
    /  \/    \    |     (   /   ‾‾\
   /          \   |  |\  \ |  (‾)  |
  / __________ \  |__| \__\ \_____/ .io

  execution: local
     script: scenario_a.js
     output: -

  scenarios: (100.00%) 1 scenario, 1000 max VUs, 6m30s max duration
           * default: Up to 1000 looping VUs for 6m0s over 4 stages

running (6m00.0s), 0000/1000 VUs, 9842 complete and 0 interrupted iterations
default ✓ [==============================] 0000/1000 VUs  6m0s

     ✓ register status 201
     ✓ register has token
     ✓ ws connected

     checks.........................: 99.83%  ✓ 29478  ✗ 51
     data_received..................: 48 MB   133 kB/s
     data_sent......................: 22 MB   60 kB/s
     http_req_blocked...............: avg=1.2ms   min=1µs    med=3µs    max=3.1s
     http_req_connecting............: avg=0.8ms   min=0µs    med=0µs    max=2.8s
   ✓ http_req_failed................: 0.17%   ✓ 51     ✗ 29478
     http_req_receiving.............: avg=58µs    min=12µs   med=37µs   max=12ms
     http_req_sending...............: avg=26µs    min=5µs    med=16µs   max=4.7ms
     http_req_tls_handshaking.......: avg=0s      min=0s     med=0s     max=0s
     http_req_waiting...............: avg=28ms    min=1.2ms  med=18ms   max=2.1s
     http_reqs......................: 19684   54.4/s
     iteration_duration.............: avg=24.4s   min=5.5s   med=22.1s  max=39s
     iterations.....................: 9842    27.2/s
   ✓ message_latency_ms.............: avg=47ms    min=2ms    med=31ms   max=980ms
                                      p(50)=31ms  p(95)=182ms  p(99)=521ms
     messages_received..............: 94387   261/s
     messages_sent..................: 94385   261/s
     vus............................: 12      min=12       max=1000
     vus_max........................: 1000    min=1000     max=1000

WARN[0360] No script output

ERRO[0360] some thresholds have failed
```

A green checkmark (`✓`) next to a metric means the threshold passed. A red
cross (`✗`) means it was breached.

---

## Metric Descriptions

### `message_latency_ms`

**Type:** Trend (percentiles)

**What it measures:** Round-trip time from when a message is sent to when it is
received back on the WebSocket connection.

- In Variants A and B the timestamp is embedded in the message content
  (`content:ts:<epoch_ms>`) and parsed on receipt.
- In Variant C the timestamp is embedded in the content of the HTTP POST body
  and parsed when the notification arrives on the WebSocket.

**Key percentiles reported:**

| Percentile | Description                                      |
|------------|--------------------------------------------------|
| p(50)      | Median – typical user experience                 |
| p(95)      | 95th percentile – worst case for most users      |
| p(99)      | 99th percentile – tail latency / outliers        |

**Thresholds:**

| Scenario  | p(50) | p(95) | p(99) |
|-----------|-------|-------|-------|
| A and B   | <100 ms | <500 ms | <1000 ms |
| C         | <200 ms | <800 ms | <2000 ms |

Variant C has a wider threshold because of the additional network hop through
RabbitMQ / notifications-service.

---

### `messages_sent`

**Type:** Counter

**What it measures:** Total number of messages sent during the test run.

- Variants A / B: incremented each time a WebSocket `type: "message"` frame is
  sent by a VU.
- Variant C: incremented each time an HTTP POST to `/api/v1/rooms/:id/messages`
  returns HTTP 201.

---

### `messages_received`

**Type:** Counter

**What it measures:** Total number of messages received over WebSocket
connections. A healthy run should show `messages_received` close to (or
slightly below) `messages_sent` × average room members, because every
room member receives a copy.

---

### `http_req_failed`

**Type:** Rate (built-in k6 metric)

**What it measures:** Fraction of HTTP requests that returned a non-2xx /
non-3xx status code, or resulted in a network error.

**Threshold:** `rate < 0.01` (less than 1 % of requests may fail).

---

### Additional Variant C metrics

| Metric | Description |
|---|---|
| `auth_latency_ms` | HTTP latency for auth-service calls |
| `rooms_latency_ms` | HTTP latency for rooms-service calls |
| `post_message_latency_ms` | HTTP latency for messages-service POST |

---

## Interpretation Guide

### Healthy run

- All thresholds show `✓`.
- `message_latency_ms` p(95) stays below the threshold even during the
  peak (1000 VUs) stage.
- `http_req_failed` rate is at or near 0.
- `messages_received` ≈ `messages_sent` × (average members per room).

### Signs of overload

| Symptom | Likely cause |
|---|---|
| p(99) `message_latency_ms` spikes sharply at high VU count | Server-side queue build-up; increase worker pool or scale horizontally |
| `http_req_failed` > 1 % | Connection pool exhaustion, OOM, or service crashing under load |
| `ws connected` check failure rate rising | WebSocket upgrade being rejected; check ephemeral port limits (`ulimit -n`) |
| `post 201` failing in Variant C | messages-service is the bottleneck; scale that service independently |
| p(95) stays low but p(99) is very high | GC pauses or lock contention; profile with pprof |

### System-level checks to run in parallel

```bash
# Watch connection counts
watch -n1 'ss -s'

# Watch CPU per process
top -d 1

# Postgres connection usage (inside container)
psql -c "SELECT count(*) FROM pg_stat_activity;"

# Kafka consumer lag (Variant B and C)
kafka-consumer-groups.sh \
  --bootstrap-server localhost:9092 \
  --describe \
  --group chat-workers
```

---

## Comparing Variants

Run all three scenarios under identical conditions and compare the JSON output.

### 1. Collect results

```bash
k6 run --out json=results_a.json scenario_a.js
k6 run --out json=results_b.json scenario_b.js
k6 run --out json=results_c.json scenario_c.js
```

### 2. Extract key percentiles with jq

```bash
for f in results_a.json results_b.json results_c.json; do
  echo "=== $f ==="
  jq -r '
    select(.type == "Point" and .metric == "message_latency_ms") |
    .data.tags.percentile + ": " + (.data.value | tostring)
  ' "$f" | sort -t: -k1,1 | uniq
done
```

### 3. Comparison table (fill in after running)

| Metric                         | Variant A | Variant B | Variant C |
|-------------------------------|-----------|-----------|-----------|
| `message_latency_ms` p(50)    |           |           |           |
| `message_latency_ms` p(95)    |           |           |           |
| `message_latency_ms` p(99)    |           |           |           |
| `http_req_failed` rate        |           |           |           |
| Peak throughput (msg/s)        |           |           |           |
| Max stable VU count           |           |           |           |
| `http_req_waiting` avg        |           |           |           |

### 4. Grafana dashboard (optional)

Start InfluxDB and Grafana, then import the official k6 dashboard
(Grafana dashboard ID **2587**):

```bash
docker-compose -f monitoring/docker-compose.yml up -d

k6 run --out influxdb=http://localhost:8086/k6 scenario_a.js
```

---

## Troubleshooting

### "dial tcp ... connection refused"

The target service is not running or is listening on a different port.

```bash
# Check what is listening
ss -tlnp | grep 8080

# Start Variant A
cd ../variant-a && docker-compose up -d
```

### "too many open files"

k6 opens one file descriptor per VU plus several per WebSocket connection.
Increase the limit before running high-VU tests:

```bash
ulimit -n 65535
# Make permanent:
echo "* soft nofile 65535" | sudo tee -a /etc/security/limits.conf
echo "* hard nofile 65535" | sudo tee -a /etc/security/limits.conf
```

### "WARN: WebSocket error: ... EOF"

Usually means the server closed the connection unexpectedly. Check server logs:

```bash
docker-compose logs -f --tail=100 server   # Variants A / B
docker-compose logs -f --tail=100 notifications-service  # Variant C
```

### "register status 201" check failing

The auth service may be returning 409 (duplicate username). Each VU appends
`Date.now()` to the username, but at very high concurrency within a single
millisecond, collisions are possible. This is expected at extreme concurrency
and does not indicate a service defect.

### High `message_latency_ms` in Variant C only

The end-to-end path in Variant C traverses RabbitMQ, adding broker latency.
Verify the broker is healthy:

```bash
# Open RabbitMQ management UI
open http://localhost:15672   # user: guest / pass: guest

# Check queue depth
rabbitmqctl list_queues name messages consumers
```

### k6 itself becoming the bottleneck

On low-spec hardware, k6 itself can saturate at 400–600 VUs. Signs include:

- CPU usage of the k6 process reaching 100 %.
- `iteration_duration` growing linearly even before the server shows distress.

Distribute the load across multiple k6 agents using k6 Cloud or
`k6 run --execution-segment`.

```bash
# Split across two machines
# Machine 1:
k6 run --execution-segment "0:1/2" --execution-segment-sequence "0,1/2,1" scenario_a.js

# Machine 2:
k6 run --execution-segment "1/2:1" --execution-segment-sequence "0,1/2,1" scenario_a.js
```
