CREATE TABLE IF NOT EXISTS tenants (
 id TEXT PRIMARY KEY,
 slug TEXT NOT NULL UNIQUE,
 name TEXT NOT NULL CHECK(length(trim(name)) > 0),
 status TEXT NOT NULL CHECK(status IN ('provisioning','active','updating','destroying','destroyed','failed')),
 version BIGINT NOT NULL DEFAULT 1,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE IF NOT EXISTS tasks (
 id TEXT PRIMARY KEY,
 tenant_id TEXT NOT NULL REFERENCES tenants(id),
 type TEXT NOT NULL CHECK(type IN ('deploy','update','destroy')),
 status TEXT NOT NULL CHECK(status IN ('accepted','in_progress','done','failed')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE UNIQUE INDEX IF NOT EXISTS one_open_task_per_tenant ON tasks(tenant_id) WHERE status IN ('accepted','in_progress');
CREATE INDEX IF NOT EXISTS tasks_tenant_idx ON tasks(tenant_id,created_at DESC);
CREATE TABLE IF NOT EXISTS outbox (
 task_id TEXT PRIMARY KEY REFERENCES tasks(id),
 payload JSONB NOT NULL,
 published_at TIMESTAMPTZ,
 attempts INT NOT NULL DEFAULT 0,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE IF NOT EXISTS inbox (
 event_id TEXT PRIMARY KEY,
 task_id TEXT NOT NULL,
 received_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
