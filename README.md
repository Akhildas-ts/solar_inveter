# Solar Monitoring Gateway

Solar Monitoring Gateway is a Go service that ingests raw telemetry from heterogeneous solar inverters, persists the unmodified payloads for audit/replay, normalizes the data into a single schema, and writes it to MongoDB or InfluxDB for querying. The system ships with an HTTP API, an in-memory cache layer, and tooling for managing field mappings between vendor-specific payloads and the canonical model.

## Current Payload Format

The default `current_format` mapping expects each POST body to contain device metadata at the root and a nested `data` object with the raw inverter readings. Both root- and nested-level fields are captured and stored as part of the raw document before mapping.

| Root Field        | Description                                 |
|-------------------|---------------------------------------------|
| `device_id`       | Unique ID for the inverter/gateway          |
| `device_name`     | Human readable name                         |
| `device_type`     | Optional hint for the mapper                |
| `device_timestamp`| RFC3339 device-side timestamp               |
| `request_id`      | Optional client supplied request GUID       |
| `data`            | Vendor payload (see below)                  |

`data` typically contains:

| Field                 | Canonical target      |
|-----------------------|-----------------------|
| `serial_no`           | `serial_no`           |
| `s1v`                 | `voltage`             |
| `total_output_power`  | `power`               |
| `f`                   | `frequency`           |
| `today_e`             | `today_energy`        |
| `total_e`             | `total_energy`        |
| `inv_temp`            | `temperature`         |
| `fault_code`          | `fault_code`          |

### Example payload

```json
{
  "device_id": "INV-001",
  "device_name": "North-Roof",
  "device_timestamp": "2025-11-17T06:45:00Z",
  "data": {
    "serial_no": "SN-777",
    "s1v": 352,
    "total_output_power": 5200,
    "f": 50,
    "today_e": 21,
    "total_e": 3412,
    "inv_temp": 42,
    "fault_code": 0
  }
}
```

## Workflow

### High-level ingestion flow
1. **HTTP intake** – `/api/data` accepts JSON and validates structure via Gin.
2. **Raw persistence** – every payload is cloned and inserted into `raw_data` with a generated `request_id`, guaranteeing replay capability before any transformation.
3. **Source detection** – the mapper inspects the payload (`source_id`, `device_type`, and signature matching) to decide which mapping rules apply.
4. **Field mapping** – defined `FieldMapping` structs translate vendor keys, enforce data types, and run optional transforms. Missing required fields fail the request (or are deferred if strict mode is disabled).
5. **Batch commit** – normalized `InverterData` structs are appended to an in-memory buffer that flushes to the configured repository (MongoDB or InfluxDB) on size or time thresholds.
6. **Statistics & caches** – ingest counters, buffer sizes, and DB counts are aggregated and cached for `/api/stats`.

### Background jobs
- **Raw reprocessor** polls `raw_data` for `processed=false` entries and retries the mapping+insert path, guaranteeing eventual consistency even if downstream services are temporarily unavailable.
- **Retention cleanup** periodically deletes processed raw documents older than 7 days.
- **Mapper auto-reload** refreshes mapping definitions every 10 s so updates through the API take effect without restarts.

## Mapping Layer

The mapping layer is backed by the `mappings` MongoDB collection. Each `DataSourceMapping` contains:

- `source_id`: logical identifier (e.g., `current_format`, `vendor_x`).
- `nested_path`: optional pointer to the sub-document containing readings.
- `mappings`: array of `FieldMapping` rules describing `source_field`, `standard_field`, `data_type`, optional `transform` (`multiply`, `divide`, `add`, `subtract`) and `required` semantics.

### Source detection

1. Respect explicit `source_id` or `device_type` hints if present and active.
2. Otherwise, flatten all top-level and nested keys and compute a similarity score against each mapping.
3. Only mappings where all required fields are present and at least 40 % of defined fields match will be considered; the highest scoring mapping wins.

### Managing mappings

- `GET /api/mappings` – list cached definitions.
- `POST /api/mappings` – create a new `DataSourceMapping`.
- `PUT /api/mappings/:id` – update an existing mapping.
- `DELETE /api/mappings/:id` – soft-delete (sets `active=false`).

All create/update/delete operations automatically invalidate the mapping cache so new rules are immediately reloaded.

## Cache Workflow

The service uses lightweight in-memory caches with independent TTLs and cleanup loops:

| Cache             | Backing store | TTL (default) | Used for                                   | Invalidation |
|-------------------|---------------|---------------|--------------------------------------------|--------------|
| `StatsCache`      | Map+mutex     | 5 s entries   | `/api/stats` response payload              | Passive TTL  |
| `QueryCache`      | Map+mutex     | 30 s entries  | `/api/records` result sets keyed by filter | Passive TTL  |
| `MappingCache`    | Map+mutex     | 5 min entries | All mappings snapshot                      | Purged when mappings mutate |
| `FaultCodesCache` | Map+mutex     | 10 min entries| Fault metadata lookups                     | Passive TTL  |

Each cache:

- stores `CacheItem{Value, Expiration}` per key;
- uses a background goroutine to evict expired keys at a cache-specific interval;
- exposes helpers to `Get`, `Set`, `Delete`, `Clear`, and `Stats`.

`CacheConfig.CloseAll()` stops every cleanup goroutine during shutdown, ensuring graceful exit.

## REST API Surface

| Method | Path                   | Description                                                       |
|--------|------------------------|-------------------------------------------------------------------|
| POST   | `/api/data`            | Ingest a raw inverter payload                                     |
| GET    | `/api/stats`           | Return processed-record stats, cache-backed                       |
| GET    | `/api/records`         | Query normalized records (filter by device, fault, time, paging)  |
| GET    | `/api/faults/codes`    | List known fault codes & suggested actions                        |
| GET    | `/api/mappings`        | Fetch active mapping definitions                                  |
| POST   | `/api/mappings`        | Create a new mapping                                              |
| PUT    | `/api/mappings/:id`    | Update mapping `:id`                                              |
| DELETE | `/api/mappings/:id`    | Soft delete mapping `:id`                                         |
| GET    | `/api/raw/stats`       | Inspect raw-data backlog, errors, cache stats                     |
| POST   | `/api/raw/reprocess/:id`| Mark a raw document for reprocessing                             |
| GET    | `/api/cache/stats`     | Introspect cache utilization                                      |
| POST   | `/api/cache/clear`     | (Future) selective cache invalidation                             |

## Configuration

| Variable              | Default                     | Purpose                                      |
|-----------------------|-----------------------------|----------------------------------------------|
| `SERVER_PORT`         | `8080`                      | HTTP listener port                           |
| `DB_TYPE`             | `mongo`                     | `mongo` or `influx` repository               |
| `MONGO_URI`           | `mongodb://localhost:27017` | MongoDB connection string                    |
| `MONGO_DATABASE`      | `solar_monitoring`          | Mongo DB name                                |
| `MONGO_COLLECTION`    | `inverter_data`             | Mongo collection for normalized data         |
| `INFLUXDB_URL`        | `http://localhost:8086`     | Influx endpoint (when `DB_TYPE=influx`)      |
| `INFLUXDB_TOKEN`      | _empty_                     | Influx API token                             |
| `INFLUXDB_DATABASE`   | `solar_monitoring`          | Target database                              |
| `BATCH_SIZE`          | `100`                       | Number of records per flush                  |
| `FLUSH_INTERVAL`      | `200` (ms)                  | Max interval between flushes                 |
| `STRICT_MAPPING_MODE` | `false`                     | Fail request on any mapping error            |
| `LOG_LEVEL`           | `INFO`                      | `INFO` or `DEBUG`                            |
| `LOG_DIRECTORY`       | `./logs`                    | Log output directory                         |

## Running Locally

1. **Dependencies** – Go 1.22+, MongoDB (for raw store & mappings), optional InfluxDB 3 (see `compose.yaml` for a ready-to-run container).
2. **Environment** – copy `.env.example` or export the variables in the table above.
3. **Install Go modules** – `go mod download`.
4. **Start services** – `docker compose up -d` (if you need Influx) and start MongoDB.
5. **Run the API** – `go run ./cmd/server`.
6. **Smoke test** – send the sample payload to `POST http://localhost:8080/api/data`.

Static files under `./static` (e.g., dashboards) are served at `/`.

## Observability & Operations

- **Logs**: structured loggers output ingestion rates, buffer depth, cache sizes, and error messages every 5 seconds.
- **Stats endpoints**: `/api/stats` (normalized data) and `/api/raw/stats` (raw queue health) are the quickest way to verify the pipeline state.
- **Graceful shutdown**: SIGINT/SIGTERM triggers a 5 s drain period that stops the HTTP server, flushes batches, closes caches, and disconnects from Mongo/Influx cleanly.
