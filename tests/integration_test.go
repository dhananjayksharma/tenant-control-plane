//go:build integration

package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dhananjayksharma/tenant-control-plane/internal/messaging"
	amqp "github.com/rabbitmq/amqp091-go"
)

var client = &http.Client{Timeout: 5 * time.Second}

func base() string {
	v := os.Getenv("API_URL")
	if v == "" {
		return "http://localhost:8080"
	}
	return v
}
func request(t *testing.T, method, path string, payload any) (int, map[string]any) {
	t.Helper()
	var body io.Reader
	if payload != nil {
		buf, e := json.Marshal(payload)
		if e != nil {
			t.Fatal(e)
		}
		body = bytes.NewReader(buf)
	}
	req, e := http.NewRequest(method, base()+path, body)
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, e := client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	var obj map[string]any
	if e := json.NewDecoder(resp.Body).Decode(&obj); e != nil {
		t.Fatal(e)
	}
	return resp.StatusCode, obj
}
func unique() string { return fmt.Sprintf("poc-%d", time.Now().UnixNano()) }
func create(t *testing.T) (string, string) {
	t.Helper()
	code, obj := request(t, "POST", "/v1/tenants", map[string]any{"slug": unique(), "name": "Demo"})
	if code != 201 {
		t.Fatalf("create status %d: %+v", code, obj)
	}
	tenant := obj["tenant"].(map[string]any)
	task := obj["task"].(map[string]any)
	return tenant["id"].(string), task["id"].(string)
}
func waitTenant(t *testing.T, id, expected string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		code, obj := request(t, "GET", "/v1/tenants/"+id, nil)
		if code == 200 && obj["status"] == expected {
			return obj
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("tenant %s did not reach %s", id, expected)
	return nil
}
func TestFullLifecycle(t *testing.T) {
	id, taskID := create(t)
	active := waitTenant(t, id, "active")
	code, task := request(t, "GET", "/v1/tasks/"+taskID, nil)
	if code != 200 || task["status"] != "done" {
		t.Fatalf("deploy task %+v", task)
	}
	version := int64(active["version"].(float64))
	code, updated := request(t, "PATCH", "/v1/tenants/"+id, map[string]any{"name": "Renamed", "version": version})
	if code != 202 {
		t.Fatalf("patch: %d %+v", code, updated)
	}
	active = waitTenant(t, id, "active")
	if active["name"] != "Renamed" {
		t.Fatal("name not updated")
	}
	code, _ = request(t, "DELETE", "/v1/tenants/"+id, nil)
	if code != 202 {
		t.Fatalf("delete status %d", code)
	}
	waitTenant(t, id, "destroyed")
	code, obj := request(t, "POST", "/v1/tenants", map[string]any{"slug": active["slug"], "name": "Duplicate"})
	if code != 409 || obj["error"].(map[string]any)["code"] != "tenant_already_exists" {
		t.Fatalf("destroyed slug reused: %d %+v", code, obj)
	}
}
func TestConcurrentPatchSameVersion(t *testing.T) {
	id, _ := create(t)
	active := waitTenant(t, id, "active")
	version := int64(active["version"].(float64))
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			reqBody, _ := json.Marshal(map[string]any{"name": fmt.Sprintf("Concurrent %d", i), "version": version})
			req, _ := http.NewRequest("PATCH", base()+"/v1/tenants/"+id, bytes.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			resp, e := client.Do(req)
			if e != nil {
				codes <- 0
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			codes <- resp.StatusCode
		}(i)
	}
	wg.Wait()
	close(codes)
	success, conflict := 0, 0
	for status := range codes {
		if status == 202 {
			success++
		}
		if status == 409 {
			conflict++
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("expected one 202 and one 409; got %d/%d", success, conflict)
	}
}
func TestConcurrentCreateSlug(t *testing.T) {
	slug := unique()
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, _ := json.Marshal(map[string]string{"slug": slug, "name": "Same"})
			req, _ := http.NewRequest("POST", base()+"/v1/tenants", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			resp, e := client.Do(req)
			if e != nil {
				codes <- 0
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			codes <- resp.StatusCode
		}()
	}
	wg.Wait()
	close(codes)
	out := []int{}
	for x := range codes {
		out = append(out, x)
	}
	if !(len(out) == 2 && ((out[0] == 201 && out[1] == 409) || (out[1] == 201 && out[0] == 409))) {
		t.Fatalf("bad duplicate outcome: %v", out)
	}
}
func TestValidationAndNotFound(t *testing.T) {
	code, obj := request(t, "POST", "/v1/tenants", map[string]string{"slug": "Bad_Name", "name": "X"})
	if code != 400 || !strings.Contains(fmt.Sprint(obj), "invalid_request") {
		t.Fatalf("invalid slug: %d %+v", code, obj)
	}
	code, obj = request(t, "GET", "/v1/tasks/unknown", nil)
	if code != 404 || !strings.Contains(fmt.Sprint(obj), "task_not_found") {
		t.Fatalf("missing task: %d %+v", code, obj)
	}
}

func TestDuplicateAndStaleProgress(t *testing.T) {
	id, taskID := create(t)
	active := waitTenant(t, id, "active")
	version := active["version"].(float64)
	url := os.Getenv("AMQP_URL")
	if url == "" {
		url = "amqp://guest:guest@localhost:5672/"
	}
	conn, err := amqp.Dial(url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer ch.Close()
	if err := messaging.Declare(ch); err != nil {
		t.Fatal(err)
	}
	for _, event := range []messaging.ProgressEvent{
		{EventID: taskID + ":done", TaskID: taskID, Status: "done"},
		{EventID: taskID + ":done", TaskID: taskID, Status: "done"},
		{EventID: "test-stale-" + taskID, TaskID: taskID, Status: "in_progress"},
	} {
		body, _ := json.Marshal(event)
		if err := messaging.Publish(ch, messaging.ProgressQueue, body); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(1500 * time.Millisecond)
	code, now := request(t, "GET", "/v1/tenants/"+id, nil)
	if code != 200 || now["status"] != "active" || now["version"] != version {
		t.Fatalf("duplicate or stale regressed state: %+v", now)
	}
	code, task := request(t, "GET", "/v1/tasks/"+taskID, nil)
	if code != 200 || task["status"] != "done" {
		t.Fatalf("task regressed: %+v", task)
	}
}
