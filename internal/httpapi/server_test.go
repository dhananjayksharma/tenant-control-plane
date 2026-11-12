package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type healthRepository struct {
	Repository
	err error
}

func (r healthRepository) PingContext(context.Context) error { return r.err }

func TestHandler(t *testing.T) {
	for _, tt := range []struct {
		name, method, path, body string
		dbErr                    error
		status                   int
		want                     string
	}{
		{"healthy", "GET", "/healthz", "", nil, 200, `"status":"ok"`},
		{"unavailable", "GET", "/healthz", "", errors.New("offline"), 503, `"code":"not_ready"`},
		{"invalid tenant", "POST", "/v1/tenants", `{"slug":"INVALID","name":"Test"}`, nil, 400, `"code":"invalid_request"`},
		{"invalid pagination", "GET", "/v1/tenants?limit=0", "", nil, 400, `"code":"invalid_request"`},
		{"invalid task path", "GET", "/v1/tasks/a/b", "", nil, 404, `"code":"task_not_found"`},
		{"unsupported method", "PUT", "/v1/tenants", "", nil, 405, `"code":"method_not_allowed"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			NewHandler(healthRepository{err: tt.dbErr}).ServeHTTP(w, req)
			if w.Code != tt.status || !strings.Contains(w.Body.String(), tt.want) {
				t.Fatalf("got %d %s; want %d containing %s", w.Code, w.Body.String(), tt.status, tt.want)
			}
			if w.Header().Get("Content-Type") != "application/json" {
				t.Fatal("expected JSON response")
			}
			if tt.status == http.StatusMethodNotAllowed && w.Header().Get("Allow") != "GET, POST" {
				t.Fatal("expected supported methods in Allow header")
			}
		})
	}
}
