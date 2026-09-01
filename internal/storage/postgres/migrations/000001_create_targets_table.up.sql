-- Mirrors target.Target (internal/target/target.go) field for field. No
-- extra columns (e.g. created_at/updated_at) are added: the Go domain
-- model doesn't have them, and M1 already decided against adding them
-- until something actually reads/writes them (see CLAUDE.md).
CREATE TABLE targets (
    id                   UUID PRIMARY KEY,
    name                 TEXT NOT NULL,
    url                  TEXT NOT NULL,
    method               TEXT NOT NULL,
    -- Interval/Timeout are stored as raw nanoseconds (BIGINT), matching
    -- time.Duration's own internal unit exactly, so there is no
    -- unit-conversion step or precision loss going to/from Go.
    interval_ns          BIGINT NOT NULL CHECK (interval_ns > 0),
    timeout_ns           BIGINT NOT NULL CHECK (timeout_ns > 0),
    expected_status_code INTEGER NOT NULL CHECK (expected_status_code BETWEEN 100 AND 599),
    enabled              BOOLEAN NOT NULL DEFAULT true
);

-- No index beyond the implicit one on the primary key (id) is added here.
-- The only other current query pattern is "list all targets, ordered by
-- name then id" (internal/api's GET /api/v1/targets, matching the
-- in-memory repository's existing ordering) — a full-table scan + sort is
-- entirely appropriate at the scale this milestone targets, and adding an
-- index to optimize an ORDER BY that has no measured cost problem would
-- be exactly the kind of premature optimization this project avoids
-- elsewhere. An index on (enabled) is a plausible future need once the
-- scheduler queries "only enabled targets" directly at the SQL level
-- instead of filtering in Go after a full List() — it doesn't do that
-- today, so it isn't added yet.
