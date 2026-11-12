package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dhananjayksharma/tenant-control-plane/internal/store"
)

var slugPattern = regexp.MustCompile(`^[a-z][-a-z0-9]{1,26}[a-z0-9]$`)

// Repository contains the persistence operations used by the HTTP API.
// The PostgreSQL implementation is internal/store.DB.
type Repository interface {
	PingContext(context.Context) error
	Create(context.Context, string, string) (store.Tenant, store.Task, error)
	ListTenants(context.Context, int, int) ([]store.Tenant, error)
	GetTenant(context.Context, string) (store.Tenant, error)
	Mutate(context.Context, string, string, string, *int64) (store.Tenant, store.Task, error)
	ListTasks(context.Context, string, int, int) ([]store.Task, error)
	GetTask(context.Context, string) (store.Task, error)
}

type server struct{ db Repository }

// NewHandler constructs the API routes using the supplied repository.
func NewHandler(db Repository) http.Handler {
	mux := http.NewServeMux()
	s := &server{db: db}
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if e := db.PingContext(r.Context()); e != nil {
			log.Printf("health check failed error=%v", e)
			problem(w, 503, "not_ready", "database unavailable")
			return
		}
		jsonResponse(w, 200, map[string]string{"status": "ok"})
		log.Print("health check succeeded database=ready")
	})
	mux.HandleFunc("/v1/tenants", s.tenants)
	mux.HandleFunc("/v1/tenants/", s.tenant)
	mux.HandleFunc("/v1/tasks", s.tasks)
	mux.HandleFunc("/v1/tasks/", s.task)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		response := &loggedResponse{ResponseWriter: w}
		defer func() {
			log.Printf("http request method=%s path=%q status=%d bytes=%d duration=%s remote=%q", r.Method, r.URL.Path, response.status, response.bytes, time.Since(start), r.RemoteAddr)
		}()
		mux.ServeHTTP(response, r)
	})
}

type loggedResponse struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *loggedResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *loggedResponse) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *loggedResponse) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(body)
	w.bytes += n
	return n, err
}

func jsonResponse(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
func problem(w http.ResponseWriter, code int, kind, message string) {
	jsonResponse(w, code, map[string]any{"error": map[string]string{"code": kind, "message": message}})
}
func storeError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, store.ErrNotFound):
		problem(w, 404, "tenant_not_found", "tenant does not exist")
	case errors.Is(e, store.ErrExists):
		problem(w, 409, "tenant_already_exists", "slug already used")
	case errors.Is(e, store.ErrVersion):
		problem(w, 409, "tenant_version_conflict", "version must match current tenant")
	case errors.Is(e, store.ErrNotAllowed):
		problem(w, 409, "tenant_update_not_allowed", "tenant state forbids operation")
	default:
		log.Printf("database error: %v", e)
		problem(w, 500, "internal_error", "unexpected server error")
	}
}
func decode(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if e := dec.Decode(v); e != nil {
		return e
	}
	var extra any
	if e := dec.Decode(&extra); e != io.EOF {
		return fmt.Errorf("unexpected trailing JSON: %v", e)
	}
	return nil
}
func pagination(r *http.Request) (int, int, error) {
	limit, offset := 50, 0
	var e error
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, e = strconv.Atoi(v)
		if e != nil || limit < 1 || limit > 200 {
			return 0, 0, errors.New("limit must be 1..200")
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		offset, e = strconv.Atoi(v)
		if e != nil || offset < 0 {
			return 0, 0, errors.New("offset must be nonnegative")
		}
	}
	return limit, offset, nil
}
func (s *server) tenants(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req struct {
			Slug string `json:"slug"`
			Name string `json:"name"`
		}
		if e := decode(r, &req); e != nil || !slugPattern.MatchString(req.Slug) || strings.TrimSpace(req.Name) == "" {
			problem(w, 400, "invalid_request", "valid slug and nonempty name required")
			return
		}
		t, task, e := s.db.Create(r.Context(), req.Slug, strings.TrimSpace(req.Name))
		if e != nil {
			storeError(w, e)
			return
		}
		w.Header().Set("Location", "/v1/tenants/"+t.ID)
		log.Printf("tenant operation accepted operation=create tenant_id=%s task_id=%s", t.ID, task.ID)
		jsonResponse(w, 201, map[string]any{"tenant": t, "task": task})
	case http.MethodGet:
		limit, offset, e := pagination(r)
		if e != nil {
			problem(w, 400, "invalid_request", e.Error())
			return
		}
		items, e := s.db.ListTenants(r.Context(), limit, offset)
		if e != nil {
			storeError(w, e)
			return
		}
		jsonResponse(w, 200, map[string]any{"items": items, "limit": limit, "offset": offset})
		log.Printf("tenants listed successfully count=%d limit=%d offset=%d", len(items), limit, offset)
	default:
		w.Header().Set("Allow", "GET, POST")
		problem(w, 405, "method_not_allowed", "unsupported HTTP method")
	}
}
func (s *server) tenant(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/v1/tenants/")
	if id == "" || strings.Contains(id, "/") {
		problem(w, 404, "tenant_not_found", "tenant does not exist")
		return
	}
	switch r.Method {
	case http.MethodGet:
		t, e := s.db.GetTenant(r.Context(), id)
		if e != nil {
			storeError(w, e)
			return
		}
		jsonResponse(w, 200, t)
		log.Printf("tenant fetched successfully tenant_id=%s", t.ID)
	case http.MethodPatch:
		var req struct {
			Name    *string `json:"name"`
			Version *int64  `json:"version"`
		}
		if e := decode(r, &req); e != nil || req.Name == nil || strings.TrimSpace(*req.Name) == "" || req.Version == nil || *req.Version < 1 {
			problem(w, 400, "invalid_request", "name and positive version required")
			return
		}
		t, task, e := s.db.Mutate(r.Context(), id, "update", strings.TrimSpace(*req.Name), req.Version)
		if e != nil {
			storeError(w, e)
			return
		}
		jsonResponse(w, 202, map[string]any{"tenant": t, "task": task})
		log.Printf("tenant operation accepted operation=update tenant_id=%s task_id=%s", t.ID, task.ID)
	case http.MethodDelete:
		t, task, e := s.db.Mutate(r.Context(), id, "destroy", "", nil)
		if e != nil {
			storeError(w, e)
			return
		}
		jsonResponse(w, 202, map[string]any{"tenant": t, "task": task})
		log.Printf("tenant operation accepted operation=destroy tenant_id=%s task_id=%s", t.ID, task.ID)
	default:
		w.Header().Set("Allow", "GET, PATCH, DELETE")
		problem(w, 405, "method_not_allowed", "unsupported HTTP method")
	}
}
func (s *server) tasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		problem(w, 405, "method_not_allowed", "read-only resource")
		return
	}
	limit, offset, e := pagination(r)
	if e != nil {
		problem(w, 400, "invalid_request", e.Error())
		return
	}
	items, e := s.db.ListTasks(r.Context(), r.URL.Query().Get("tenant_id"), limit, offset)
	if e != nil {
		storeError(w, e)
		return
	}
	jsonResponse(w, 200, map[string]any{"items": items, "limit": limit, "offset": offset})
	log.Printf("tasks listed successfully count=%d limit=%d offset=%d tenant_id=%q", len(items), limit, offset, r.URL.Query().Get("tenant_id"))
}
func (s *server) task(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		problem(w, 405, "method_not_allowed", "read-only resource")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/tasks/")
	if id == "" || strings.Contains(id, "/") {
		problem(w, 404, "task_not_found", "task does not exist")
		return
	}
	t, e := s.db.GetTask(r.Context(), id)
	if errors.Is(e, store.ErrNotFound) {
		problem(w, 404, "task_not_found", "task does not exist")
		return
	}
	if e != nil {
		storeError(w, e)
		return
	}
	jsonResponse(w, 200, t)
	log.Printf("task fetched successfully task_id=%s", t.ID)
}
