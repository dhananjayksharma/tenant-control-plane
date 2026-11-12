package core

import "testing"

func TestTenantTerminalStates(t *testing.T) {
	cases := []struct {
		from   TenantStatus
		task   string
		result TaskStatus
		want   TenantStatus
	}{
		{"provisioning", "deploy", Done, "active"}, {"provisioning", "deploy", FailedTask, "failed"},
		{"updating", "update", Done, "active"}, {"updating", "update", FailedTask, "failed"},
		{"destroying", "destroy", Done, "destroyed"}, {"destroying", "destroy", FailedTask, "failed"},
	}
	for _, c := range cases {
		got, err := NextTenantStatus(c.from, c.task, c.result)
		if err != nil || got != c.want {
			t.Fatalf("%+v: got %s err %v", c, got, err)
		}
	}
	if _, err := NextTenantStatus("active", "deploy", Done); err == nil {
		t.Fatal("invalid state accepted")
	}
}
func TestMonotonicProgress(t *testing.T) {
	if !AcceptProgress(Accepted, InProgress) || !AcceptProgress(InProgress, Done) || !AcceptProgress(Accepted, Done) {
		t.Fatal("valid progress rejected")
	}
	for _, s := range []TaskStatus{Done, FailedTask} {
		for _, next := range []TaskStatus{InProgress, Done, FailedTask} {
			if AcceptProgress(s, next) {
				t.Fatalf("regression %s to %s", s, next)
			}
		}
	}
}
