package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dhananjayksharma/tenant-control-plane/internal/core"
	"github.com/dhananjayksharma/tenant-control-plane/internal/messaging"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed schema.sql
var migrations embed.FS

type DB struct{ *sql.DB }
type Tenant struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
type Task struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	TenantID  string    `json:"tenant_id"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// MarshalJSON guarantees the assignment's millisecond-precision UTC timestamps.
func (t Tenant) MarshalJSON() ([]byte, error) {
	type alias Tenant
	return json.Marshal(struct {
		alias
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
	}{alias(t), t.CreatedAt.UTC().Format("2006-01-02T15:04:05.000Z"), t.UpdatedAt.UTC().Format("2006-01-02T15:04:05.000Z")})
}
func (t Task) MarshalJSON() ([]byte, error) {
	type alias Task
	return json.Marshal(struct {
		alias
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
	}{alias(t), t.CreatedAt.UTC().Format("2006-01-02T15:04:05.000Z"), t.UpdatedAt.UTC().Format("2006-01-02T15:04:05.000Z")})
}

var ErrNotFound = errors.New("not found")
var ErrExists = errors.New("already exists")
var ErrNotAllowed = errors.New("update not allowed")
var ErrVersion = errors.New("version conflict")

func Open(url string) (*DB, error) {
	db, e := sql.Open("pgx", url)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if e = db.PingContext(ctx); e != nil {
		db.Close()
		return nil, e
	}
	return &DB{db}, nil
}
func (d *DB) Migrate(ctx context.Context) error {
	s, e := migrations.ReadFile("schema.sql")
	if e != nil {
		return e
	}
	_, e = d.ExecContext(ctx, string(s))
	return e
}
func uniqueError(err error) bool {
	var p *pgconn.PgError
	return errors.As(err, &p) && p.Code == "23505"
}
func scanTenant(row interface{ Scan(...any) error }) (Tenant, error) {
	var t Tenant
	err := row.Scan(&t.ID, &t.Slug, &t.Name, &t.Status, &t.Version, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}
func scanTask(row interface{ Scan(...any) error }) (Task, error) {
	var t Task
	err := row.Scan(&t.ID, &t.Type, &t.TenantID, &t.Status, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}

const tenantCols = "id,slug,name,status,version,created_at,updated_at"
const taskCols = "id,type,tenant_id,status,created_at,updated_at"

func taskEvent(task Task) ([]byte, error) {
	return json.Marshal(messaging.TaskEvent{ID: task.ID, Type: task.Type, TenantID: task.TenantID, Status: task.Status})
}
func insertTask(ctx context.Context, tx *sql.Tx, tenantID, typ string) (Task, error) {
	task, e := scanTask(tx.QueryRowContext(ctx, "INSERT INTO tasks(id,tenant_id,type,status) VALUES (gen_random_uuid()::text,$1,$2,'accepted') RETURNING "+taskCols, tenantID, typ))
	if e != nil {
		return Task{}, e
	}
	payload, e := taskEvent(task)
	if e != nil {
		return Task{}, e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO outbox(task_id,payload) VALUES ($1,$2)", task.ID, string(payload))
	return task, e
}
func (d *DB) Create(ctx context.Context, slug, name string) (Tenant, Task, error) {
	tx, e := d.BeginTx(ctx, nil)
	if e != nil {
		return Tenant{}, Task{}, e
	}
	defer tx.Rollback()
	t, e := scanTenant(tx.QueryRowContext(ctx, "INSERT INTO tenants(id,slug,name,status) VALUES (gen_random_uuid()::text,$1,$2,'provisioning') RETURNING "+tenantCols, slug, name))
	if uniqueError(e) {
		return Tenant{}, Task{}, ErrExists
	}
	if e != nil {
		return Tenant{}, Task{}, e
	}
	task, e := insertTask(ctx, tx, t.ID, "deploy")
	if e != nil {
		return Tenant{}, Task{}, e
	}
	if e = tx.Commit(); e != nil {
		return Tenant{}, Task{}, e
	}
	return t, task, nil
}
func (d *DB) GetTenant(ctx context.Context, id string) (Tenant, error) {
	t, e := scanTenant(d.QueryRowContext(ctx, "SELECT "+tenantCols+" FROM tenants WHERE id=$1", id))
	if errors.Is(e, sql.ErrNoRows) {
		return Tenant{}, ErrNotFound
	}
	return t, e
}
func (d *DB) ListTenants(ctx context.Context, limit, offset int) ([]Tenant, error) {
	rows, e := d.QueryContext(ctx, "SELECT "+tenantCols+" FROM tenants ORDER BY created_at,id LIMIT $1 OFFSET $2", limit, offset)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	result := []Tenant{}
	for rows.Next() {
		t, e := scanTenant(rows)
		if e != nil {
			return nil, e
		}
		result = append(result, t)
	}
	return result, rows.Err()
}
func (d *DB) GetTask(ctx context.Context, id string) (Task, error) {
	t, e := scanTask(d.QueryRowContext(ctx, "SELECT "+taskCols+" FROM tasks WHERE id=$1", id))
	if errors.Is(e, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	return t, e
}
func (d *DB) ListTasks(ctx context.Context, tenantID string, limit, offset int) ([]Task, error) {
	q := "SELECT " + taskCols + " FROM tasks"
	args := []any{}
	if tenantID != "" {
		q += " WHERE tenant_id=$1"
		args = append(args, tenantID)
	}
	index := len(args) + 1
	q += fmt.Sprintf(" ORDER BY created_at,id LIMIT $%d OFFSET $%d", index, index+1)
	args = append(args, limit, offset)
	rows, e := d.QueryContext(ctx, q, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	result := []Task{}
	for rows.Next() {
		t, e := scanTask(rows)
		if e != nil {
			return nil, e
		}
		result = append(result, t)
	}
	return result, rows.Err()
}

// Mutate locks the tenant row to serialize competing API updates and worker outcomes.
func (d *DB) Mutate(ctx context.Context, id, operation, name string, version *int64) (Tenant, Task, error) {
	tx, e := d.BeginTx(ctx, nil)
	if e != nil {
		return Tenant{}, Task{}, e
	}
	defer tx.Rollback()
	t, e := scanTenant(tx.QueryRowContext(ctx, "SELECT "+tenantCols+" FROM tenants WHERE id=$1 FOR UPDATE", id))
	if errors.Is(e, sql.ErrNoRows) {
		return Tenant{}, Task{}, ErrNotFound
	}
	if e != nil {
		return Tenant{}, Task{}, e
	}
	if operation == "update" {
		if version == nil || *version != t.Version {
			return Tenant{}, Task{}, ErrVersion
		}
		if t.Status != "active" {
			return Tenant{}, Task{}, ErrNotAllowed
		}
	} else if operation == "destroy" {
		if t.Status != "active" && t.Status != "failed" {
			return Tenant{}, Task{}, ErrNotAllowed
		}
	} else {
		return Tenant{}, Task{}, ErrNotAllowed
	}
	next := "updating"
	typ := "update"
	if operation == "destroy" {
		next = "destroying"
		typ = "destroy"
		name = t.Name
	}
	t, e = scanTenant(tx.QueryRowContext(ctx, "UPDATE tenants SET name=$2,status=$3,version=version+1,updated_at=clock_timestamp() WHERE id=$1 RETURNING "+tenantCols, id, name, next))
	if e != nil {
		return Tenant{}, Task{}, e
	}
	task, e := insertTask(ctx, tx, id, typ)
	if e != nil {
		return Tenant{}, Task{}, e
	}
	if e = tx.Commit(); e != nil {
		return Tenant{}, Task{}, e
	}
	return t, task, nil
}

// ApplyProgress commits the inbox dedupe key and state changes in the SAME transaction.
func (d *DB) ApplyProgress(ctx context.Context, p messaging.ProgressEvent) error {
	if strings.TrimSpace(p.EventID) == "" || strings.TrimSpace(p.TaskID) == "" {
		return errors.New("missing event_id or task_id")
	}
	incoming := core.TaskStatus(p.Status)
	if incoming != core.InProgress && incoming != core.Done && incoming != core.FailedTask {
		return errors.New("invalid progress status")
	}
	tx, e := d.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	// Consistent tenant-first lock ordering with API requests avoids lock inversion.
	var tenantID string
	e = tx.QueryRowContext(ctx, "SELECT tenant_id FROM tasks WHERE id=$1", p.TaskID).Scan(&tenantID)
	if errors.Is(e, sql.ErrNoRows) {
		return errors.New("unknown task id")
	}
	if e != nil {
		return e
	}
	var status string
	e = tx.QueryRowContext(ctx, "SELECT status FROM tenants WHERE id=$1 FOR UPDATE", tenantID).Scan(&status)
	if e != nil {
		return e
	}
	task, e := scanTask(tx.QueryRowContext(ctx, "SELECT "+taskCols+" FROM tasks WHERE id=$1 FOR UPDATE", p.TaskID))
	if e != nil {
		return e
	}
	var inserted string
	e = tx.QueryRowContext(ctx, "INSERT INTO inbox(event_id,task_id) VALUES ($1,$2) ON CONFLICT DO NOTHING RETURNING event_id", p.EventID, p.TaskID).Scan(&inserted)
	if errors.Is(e, sql.ErrNoRows) {
		return tx.Commit()
	}
	if e != nil {
		return e
	}
	if !core.AcceptProgress(core.TaskStatus(task.Status), incoming) {
		return tx.Commit()
	}
	_, e = tx.ExecContext(ctx, "UPDATE tasks SET status=$2,updated_at=clock_timestamp() WHERE id=$1", p.TaskID, p.Status)
	if e != nil {
		return e
	}
	if core.IsTerminal(incoming) {
		next, e := core.NextTenantStatus(core.TenantStatus(status), task.Type, incoming)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, "UPDATE tenants SET status=$2,version=version+1,updated_at=clock_timestamp() WHERE id=$1", tenantID, string(next))
		if e != nil {
			return e
		}
	}
	return tx.Commit()
}

type Pending struct {
	TaskID  string
	Payload []byte
}

func (d *DB) Pending(ctx context.Context) ([]Pending, error) {
	rows, e := d.QueryContext(ctx, "SELECT task_id,payload FROM outbox WHERE published_at IS NULL ORDER BY created_at LIMIT 100")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	r := []Pending{}
	for rows.Next() {
		var p Pending
		if e = rows.Scan(&p.TaskID, &p.Payload); e != nil {
			return nil, e
		}
		r = append(r, p)
	}
	return r, rows.Err()
}
func (d *DB) MarkPublished(ctx context.Context, id string) error {
	_, e := d.ExecContext(ctx, "UPDATE outbox SET published_at=clock_timestamp(), attempts=attempts+1 WHERE task_id=$1 AND published_at IS NULL", id)
	return e
}
