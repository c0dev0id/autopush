package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsPassing(t *testing.T) {
	cases := map[string]bool{
		"success":         true,
		"skipped":         true,
		"neutral":         true,
		"failure":         false,
		"cancelled":       false,
		"timed_out":       false,
		"action_required": false,
		"startup_failure": false,
		"":                false,
	}
	for conclusion, want := range cases {
		if got := isPassing(conclusion); got != want {
			t.Errorf("isPassing(%q) = %v, want %v", conclusion, got, want)
		}
	}
}

func TestReportSettled(t *testing.T) {
	if got := reportSettled("test", true); got != 2 {
		t.Errorf("any failed: exit code %d, want 2", got)
	}
	if got := reportSettled("test", false); got != 0 {
		t.Errorf("all passed: exit code %d, want 0", got)
	}
}

func TestGetJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" || r.Header.Get("Accept") != "application/vnd.github+json" {
			http.Error(w, "bad headers", http.StatusForbidden)
			return
		}
		w.Write([]byte(`{"id": 7, "name": "ci", "status": "completed", "conclusion": "success"}`))
	}))
	defer srv.Close()

	var run workflowRun
	if err := getJSON(context.Background(), srv.Client(), srv.URL, "tok", &run); err != nil {
		t.Fatal(err)
	}
	if run.ID != 7 || run.Name != "ci" || run.Conclusion != "success" {
		t.Errorf("decoded %+v", run)
	}

	err := getJSON(context.Background(), srv.Client(), srv.URL, "wrong", &run)
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("bad token: err = %v, want a 403 error", err)
	}
}
