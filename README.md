# SafelyYou Fleet Metrics — Coding Challenge

A small Go HTTP service that ingests heartbeat and upload-time telemetry
from a fleet of edge devices and computes per-device uptime % and average
upload duration on demand. Implements the contract in `spec/openapi.json`.

The simulator agrees on every metric for every device — see [`results.txt`](./results.txt).

```text
DeviceID                  Uptime      AvgUploadTime
60-6b-44-84-dc-64         99.79167    3m7.893379134s
b4-45-52-a2-f1-3c         100.00000   3m19.085533836s
26-9a-66-01-33-83         92.91667    3m21.858747766s
18-b8-87-e7-1f-06         98.75000    3m17.331667813s
38-4e-73-e0-33-59         99.79167    3m29.226522788s
```

## Quick start

Requires Go 1.22+.

```bash
make build              # compiles ./bin/server
make run                # runs on :6733 with data/devices.csv

# In another shell, fetch the simulator and run it:
mkdir -p bin
curl -fsSL -o bin/device-simulator \
  https://sy-fleet-interview-assets.s3.us-east-2.amazonaws.com/device-simulator-mac-arm64
chmod +x bin/device-simulator
./bin/device-simulator -port 6733
```

Other simulator platforms: `-mac-amd64`, `-linux-amd64`, `-linux-arm64`,
`-win-amd64`, `-win-arm64` (same URL prefix).

Server flags: `-port` (default `6733`), `-csv` (default `data/devices.csv`).
Bind address is fixed to `127.0.0.1`.

```bash
curl -s http://127.0.0.1:6733/healthz   # -> ok
```

## Project layout

```
.
├── cmd/server/main.go          # entry point
├── internal/
│   ├── api/                    # HTTP layer (server / handlers / middleware / httpio)
│   ├── devices/                # CSV-backed device set
│   └── telemetry/              # per-device aggregator + fleet store
├── spec/openapi.json           # vendored API contract
├── data/devices.csv            # fleet definition (5 devices)
└── results.txt                 # latest simulator output
```

## Architecture

```
   device-simulator
   (5 goroutines, ~2860 POSTs in parallel)
          │ HTTP /api/v1
          ▼
   api package (net/http, mux)
   - validates device_id
   - decodes JSON (1 MiB cap)
   - returns 204 / 404 / 500
          │
   ┌──────┴──────┐
   ▼             ▼
 devices.       telemetry.
 Registry        Store
 (read-only)    (per-device aggregator + mutex)
```

Three packages, one job each. `api` depends on the other two through
narrow interfaces, so handlers are unit-testable without a real CSV file
or aggregator state.

## The uptime formula

Per the spec:

```
uptime = (sumHeartbeats / numMinutesBetweenFirstAndLastHeartbeat) * 100
```

`numMinutesBetweenFirstAndLastHeartbeat` is genuinely ambiguous — it could
mean `lastMin - firstMin` (intervals between endpoints) or `lastMin -
firstMin + 1` (slots inclusive). I use the first reading, **verified
experimentally**: patching the formula to `+ 1` and running the simulator
caused all 5 devices to mismatch by a small consistent offset
(e.g. `99.79167` vs `99.58420` — exactly `479/480` vs `479/481`). The
exclusive reading is what the simulator's reference implementation uses.

## Testing

```bash
make test         # all package tests
make test-race    # race detector
```

Three test files, one per package:

- `devices/registry_test.go` — CSV parsing happy path + dedup + empty
  cases, plus a smoke test against the real fixture.
- `telemetry/aggregator_test.go` — uptime math edge cases (single,
  partial, the >100% boundary, duplicate-within-minute, out-of-order),
  per-device isolation, and a 64-goroutine × 500-iteration concurrent
  write race test.
- `api/api_test.go` — handler status codes, error body shape, end-to-end
  happy path, method mismatch (405), malformed-body (500).

## Write-up

### Time spent and most difficult part

Approximately 4 hours.

The hardest part was doing AI-assisted coding in a language I didn't
already know. Go has idioms I hadn't seen before, and every one
required cross-checking to make sure the AI wasn't producing
plausible-looking-but-incorrect output and that the result would read
cleanly to a Go-fluent reviewer.

A smaller correctness puzzle showed up in the spec's uptime formula —
"minutes between first and last heartbeat" admits two readings, and I
confirmed which one the simulator uses by patching the alternative and
observing the resulting mismatch.

### Adding more kinds of metrics

Today the aggregator hardcodes two metrics: a unique-minute set for
uptime and a running sum + count for the mean upload time. Two paths
scale up:

1. **Direct extension** — add fields to the aggregator for one or two
   more metrics. Cheapest; doesn't generalize.
2. **Strategy pattern** — define a `Metric` interface, let the
   aggregator hold `map[string]Metric`:

   ```go
   type Metric interface {
       Record(sample any)
       Snapshot() (key string, value any)
   }

   type aggregator struct {
       mu      sync.Mutex
       metrics map[string]Metric
   }
   ```

   The HTTP layer iterates the map and returns `{name: value, ...}`.
   Adding a metric type means writing a new `Metric` implementation;
   handlers and store stay untouched.

At larger fleet scale, the right answer is to stop computing in-process
— push events to a time-series store and let queries compute the
aggregates. The service becomes thin ingest, and ad-hoc metrics no
longer need code changes.

### Runtime complexity

Per request:

| Endpoint | Cost |
|---|---|
| `POST /heartbeat` | **O(1)** — map lookup, mutex acquire, map insert |
| `POST /stats` | **O(1)** — map lookup, mutex acquire, two integer adds |
| `GET /stats` | **O(M)** where M = unique heartbeat minutes for the device |

Memory per device: O(M) for the heartbeat-minute set, O(1) for upload
sum/count. For an 8-hour run, M ≈ 480.

**Is it optimal?** One deliberate suboptimization. `GET /stats` could be
O(1) by tracking running first/last fields updated on every heartbeat
write. I removed that bookkeeping because writes are frequent and
snapshots are rare; paying linearly on the rare path keeps the write
code simpler.

Memory could be O(1) per device by dropping the heartbeat set entirely,
but the spec requires "duplicate heartbeats in one minute count as one"
and the set is the cheapest way to enforce that.

Other dimensions are at their natural lower bounds: device lookups are
O(1), JSON decode is bounded, routing is O(1), and per-device locks let
unrelated devices proceed concurrently with no possibility of deadlock.

## How I used AI

Claude provided the initial scaffolding. I picked the architecture and
wrote it with AI-assisted coding, reviewing each line and
double-checking each idiom.
