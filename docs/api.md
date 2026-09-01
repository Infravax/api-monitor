# API Reference (Milestone 2)

Base path: `/api/v1`. All request and response bodies are JSON.

This covers target management only. Check results, incidents, and alerts
are not implemented yet (see `docs/roadmap.md`).

## Health

### `GET /health`

Liveness only — confirms the process is up and serving HTTP. It does not
check PostgreSQL connectivity, even though a real database dependency
exists as of M6: a proper liveness/readiness split is deferred to M10
(Observability), a deliberate scope decision rather than an oversight.
Container startup ordering (waiting for PostgreSQL before starting the
application) is instead handled by Docker Compose's own healthcheck on
the `postgres` container plus `depends_on: condition: service_healthy` —
see `docs/architecture.md` — which doesn't require this endpoint to
change at all.

```
200 OK
{"status": "ok"}
```

## Targets

### Target representation

```json
{
  "id": "ac7f4482-598b-499b-b3ed-445af678a4a8",
  "name": "Example API",
  "url": "https://example.com/health",
  "method": "GET",
  "interval": "30s",
  "timeout": "5s",
  "expected_status_code": 200,
  "enabled": true
}
```

- `id` — server-generated, read-only.
- `interval` / `timeout` — Go duration strings (`"30s"`, `"1m"`, `"1m30s"`).
  Accepted formats are whatever `time.ParseDuration` accepts.
- `method` is normalized to uppercase; allowed values: `GET`, `POST`,
  `PUT`, `PATCH`, `DELETE`, `HEAD`, `OPTIONS`.

### `POST /api/v1/targets` — create a target

Request body: the target representation, without `id`.

- `method` defaults to `GET` if omitted.
- `expected_status_code` defaults to `200` if omitted.
- `enabled` defaults to `true` if omitted.
- `interval` and `timeout` have **no default** — both are required and
  must be a positive duration.

```
201 Created
<target representation, including the generated id>
```

`400 Bad Request` if the body is malformed JSON, a duration string can't be
parsed, or the resulting target fails domain validation (empty name, empty
or invalid URL, unsupported URL scheme, unsupported method, non-positive
interval/timeout, or an expected status code outside 100–599).

### `GET /api/v1/targets` — list targets

```
200 OK
{"targets": [ <target representation>, ... ]}
```

Ordered by `name`, then `id`, for a deterministic result (the domain model
has no creation timestamp to order by — see `docs/architecture.md`).
`targets` is always present, as `[]` when there are none.

### `GET /api/v1/targets/{id}` — get a target

```
200 OK
<target representation>
```

`404 Not Found` if no target with that ID exists.

### `PUT /api/v1/targets/{id}` — replace a target

**Full replacement**, not a partial update: send the complete target
representation, not just the fields you're changing. Defaulting rules are
the same as create — in particular, omitting `enabled` sets it to `true`,
even if the target was previously disabled.

```
200 OK
<updated target representation, same id>
```

`404 Not Found` if no target with that ID exists.
`400 Bad Request` for the same validation reasons as create.

### `DELETE /api/v1/targets/{id}` — delete a target

```
204 No Content
```

`404 Not Found` if no target with that ID exists.

## Errors

All error responses share one shape:

```json
{
  "error": {
    "code": "INVALID_REQUEST",
    "message": "target: url must be an absolute url with a host"
  }
}
```

| HTTP status | `code` | Meaning |
|---|---|---|
| 400 | `INVALID_REQUEST` | Malformed JSON, an unparsable duration, or a domain validation failure. `message` is the specific reason. |
| 404 | `NOT_FOUND` | No target with the given ID. |
| 409 | `INVALID_REQUEST` | Target ID already exists (server-generated IDs make this practically unreachable, but the repository guards against it). |
| 500 | `INTERNAL_ERROR` | Unexpected server-side failure. `message` is intentionally generic — details are logged server-side with the request ID, never sent to the client. |

Every response includes an `X-Request-ID` header; it also appears in
server logs for that request, useful for correlating a client-reported
error with server logs.
