# OpenTelemetry Transform Processor — Lab Notes

This experiment explores the OTel Collector's `transform` processor using OTTL (OpenTelemetry
Transformation Language) to parse structured JSON logs and promote their fields into proper OTLP
attributes — severity, trace correlation, and more.

Reference article: https://www.dash0.com/guides/opentelemetry-transform-processor

---

## What Changed

### `emissor-de-metricas-simples/main.go`

The app now writes **structured JSON logs** to a file instead of plain-text stdout:

```json
{"level":"INFO","message":"emitindo metrica OTel...","timestamp":"2026-03-16T18:41:38Z"}
{"level":"ERROR","message":"request falhou: caminho /erro acionado","timestamp":"2026-03-16T18:41:55Z"}
{"level":"DEBUG","message":"processando caminho: sre","timestamp":"2026-03-16T18:41:38Z"}
```

Three env vars control runtime configuration (with localhost defaults for local `go run`):

| Variable                  | Default           | Docker value       |
|---------------------------|-------------------|--------------------|
| `OTEL_COLLECTOR_ADDRESS`  | `localhost:4317`  | `collector:4317`   |
| `OTEL_TRACES_ADDRESS`     | `localhost:4417`  | `collector:4417`   |
| `LOG_FILE_PATH`           | `minhaApp.log`    | `/var/log/sre/minhaApp.log` |

### `collector-config.yaml` — the transform processor

```yaml
processors:
  transform:
    error_mode: ignore        # log errors per-statement, never drop the whole batch
    log_statements:
      - context: log          # "log" context: paths like body, attributes, severity_text, cache
        conditions:
          - IsString(body) and IsMatch(body, "^\\s*\\{")   # only JSON bodies
        statements:
          - merge_maps(cache, ParseJSON(body), "upsert")          # 1. parse into scratch space
          - merge_maps(attributes, cache, "upsert")               # 2. promote all fields as attributes
          - set(severity_text, cache["level"]) where cache["level"] != nil   # 3. -> severity_text
          - set(severity_number, SEVERITY_NUMBER_DEBUG) where cache["level"] == "DEBUG"  # 4. -> severity_number
          - set(severity_number, SEVERITY_NUMBER_INFO)  where cache["level"] == "INFO"
          - set(severity_number, SEVERITY_NUMBER_WARN)  where cache["level"] == "WARN"
          - set(severity_number, SEVERITY_NUMBER_ERROR) where cache["level"] == "ERROR"
          - set(trace_id.string, cache["trace_id"]) where cache["trace_id"] != nil  # 5. log-trace correlation
          - set(span_id.string,  cache["span_id"])  where cache["span_id"]  != nil
```

**Key OTTL concepts demonstrated:**

- **`context: log`** — declares which OTLP hierarchy level the statements operate on. Paths inside
  are relative: `body`, `attributes`, `severity_text`, etc. (no `log.` prefix needed).
- **`conditions:`** — guard clause evaluated before any statement runs. The whole block is skipped
  if no condition matches. Uses `IsString` + `IsMatch` to detect JSON bodies.
- **`cache`** — a per-record scratch space that exists only during evaluation. Used here as an
  intermediate buffer: parse JSON into `cache`, then copy out to real OTLP fields.
- **`ParseJSON(body)`** — built-in OTTL function that returns a `pcommon.Map` from a JSON string.
- **`merge_maps(dest, src, "upsert")`** — copies all keys from `src` into `dest`, overwriting on
  collision. The `"upsert"` strategy is one of `insert`, `update`, or `upsert`.
- **`SEVERITY_NUMBER_*`** — built-in OTTL enum constants mapping to the OTLP severity number spec
  (DEBUG=5, INFO=9, WARN=13, ERROR=17).
- **`trace_id.string`** — the `.string` suffix tells OTTL to accept a hex string and auto-convert
  to the typed `TraceID` field, enabling Loki → Tempo log-to-trace correlation.
- **`where` clause** — per-statement guard, evaluated lazily. Prevents nil-dereference errors and
  lets you apply statements conditionally without nested logic.
- **`error_mode: ignore`** — recommended for production; logs OTTL errors per record and moves on
  rather than dropping entire batches (`propagate`) or silently skipping (`silent`).

### `docker-compose.yml`

- Added `metal-app` service built from `emissor-de-metricas-simples/Dockerfile`.
- Introduced a shared named volume `app-logs` mounted at `/var/log/sre` in both `metal-app`
  (writer) and `collector` (reader), replacing the previous host bind-mount.
- Pinned `grafana/tempo:2.6.1` — the `latest` image (v2.10.2) introduced a breaking Kafka
  requirement incompatible with this single-binary config.

---

## Running the Lab

```bash
# Bring everything up (builds the Go app image on first run)
docker-compose up --build

# Trigger some log events
curl http://localhost:8080/sre        # INFO + DEBUG logs
curl http://localhost:8080/erro       # INFO + ERROR logs
```

---

## Confirming the Transformation

### 1. Check the raw log file (inside the container)

```bash
docker exec metal-app-metal-app-1 cat /var/log/sre/minhaApp.log
```

You should see one JSON object per line.

### 2. Query Loki directly

```bash
# Check available labels (should include service_name, level after transform)
curl http://localhost:3100/loki/api/v1/labels

# Query logs for the app
curl 'http://localhost:3100/loki/api/v1/query_range?query={service_name="metal-app"}&limit=10'
```

### 3. Grafana Explore

Open `http://localhost:3000` (admin / admin) → Explore → select **Loki** datasource.

- Run `{service_name="metal-app"}` — you should see log lines with structured metadata.
- Filter by severity: `{service_name="metal-app"} | severity_text = "ERROR"`.
- The `level`, `message`, and `timestamp` fields should appear as log attributes (promoted by
  `merge_maps(attributes, cache, "upsert")`).
- `severity_text` and `severity_number` should be set (visible in the log detail panel).

### 4. Verify the transform is running in the collector

```bash
docker logs collector 2>&1 | grep -i transform
```

No output here is actually good — `error_mode: ignore` means silent success. Any OTTL parse errors
would appear as `warn` lines.

---

## Troubleshooting

### `unknown context` error on collector startup

```
Error: invalid configuration: processors::transform: unknown context
```

Each statement group under `log_statements:` must have an explicit `context: log` field.
Without it, the processor doesn't know which OTLP hierarchy level to operate on.
OTTL paths inside the block are then relative (e.g. `body`, not `log.body`).

### Loki shows `service_name=unknown_service` instead of `metal-app`

When using the `otlphttp` exporter to Loki's OTLP endpoint (`/otlp/v1/logs`), Loki resolves
the service name from the **OTel semantic convention attribute `service.name`** (dot notation).
The resource processor must use the dot-notation OTel semantic convention:

```yaml
- key: service.name    # OTel semantic convention — used by OTLP receivers including Loki
  value: "metal-app"
  action: upsert
```

The underscore form (`service_name`) and the `loki.resource.labels` hint were only needed
for the old Loki native-push exporter format. They are no-ops when using `otlphttp`.

### `otlphttp` exporter appends `/v1/logs` to the endpoint

The `otlphttp` exporter automatically appends `/v1/logs` to whatever `endpoint:` you configure.
Loki's OTLP ingestion URL is `/otlp/v1/logs`, so the correct base endpoint is:

```yaml
otlphttp/loki:
  endpoint: http://loki:3100/otlp    # results in http://loki:3100/otlp/v1/logs
```

Setting `endpoint: http://loki:3100/loki/api/v1/push` would produce the path
`/loki/api/v1/push/v1/logs`, which returns HTTP 404.

### `exec /app/main: no such file or directory` on Alpine

Go binaries compiled with CGO enabled (the default) dynamically link against glibc.
Alpine uses musl libc — these binaries won't execute there. Fix: build with `CGO_ENABLED=0`
to produce a fully static binary that runs on any Linux base image including Alpine or `scratch`.

### Grafana/tempo image startup failure: "the Kafka topic has not been configured"

Grafana Tempo's `latest` Docker tag moved to v2.10+ which introduced a mandatory Kafka
distributor configuration. Pin the image to a working version:

```yaml
tempo:
  image: grafana/tempo:2.6.1
```

### filelog receiver logs "no files match the configured criteria"

This warning at startup is normal when the app container hasn't written its first log yet.
The receiver polls on a short interval and will start watching as soon as the file appears.

---

## Pipeline Flow

```
minhaApp.log (JSON lines)
    │
    ▼
filelog receiver          # tails /var/log/sre/minhaApp.log, emits raw log records
    │
    ▼
transform processor       # OTTL: parse JSON body → promote level, severity, trace_id
    │
    ▼
resource processor        # adds service_name, deployment_environment labels
    │
    ▼
otlphttp/loki exporter    # pushes to Loki at http://loki:3100/loki/api/v1/push
```
