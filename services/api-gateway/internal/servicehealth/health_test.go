package servicehealth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func number(v float64) *float64 { return &v }
func TestClassify(t *testing.T) {
	for _, tc := range []struct {
		name    string
		service Service
		want    string
	}{
		{"missing", Service{}, "Unknown"},
		{"down", Service{Up: number(0)}, "Critical"},
		{"idle", Service{Up: number(1), RPS: number(0)}, "Unknown"},
		{"partial", Service{Up: number(1), RPS: number(2)}, "Unknown"},
		{"healthy", Service{Up: number(1), RPS: number(2), ErrorRate: number(0), P95MS: number(100)}, "Healthy"},
		{"errors", Service{Up: number(1), RPS: number(2), ErrorRate: number(.05), P95MS: number(100)}, "Critical"},
		{"slow", Service{Up: number(1), RPS: number(2), ErrorRate: number(0), P95MS: number(1000)}, "Degraded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := classify(tc.service)
			if got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}
func TestRead(t *testing.T) {
	var instant string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query" || r.URL.Query().Get("timeout") != "1s" {
			t.Error("invalid request")
		}
		at := r.URL.Query().Get("time")
		if instant != "" && instant != at {
			t.Error("queries must share evaluation time")
		}
		instant = at
		v := "1"
		switch r.URL.Query().Get("query") {
		case queries[0]:
			v = "1"
		case queries[1]:
			v = "2"
		case queries[2]:
			v = "0"
		case queries[3]:
			v = "125"
		default:
			t.Error("unexpected query")
		}
		stamp, _ := time.Parse(time.RFC3339Nano, at)
		_, _ = fmt.Fprintf(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"service":"payment-service"},"value":[%d,"%s"]}]}}`, stamp.Unix(), v)
	}))
	defer server.Close()
	client, err := New(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Scope != "shared-runtime" || len(got.Items) != 7 || got.Items[0].Status != "Unknown" || got.Items[4].Status != "Healthy" || *got.Items[4].P95MS != 125 {
		t.Fatalf("unexpected snapshot %+v", got)
	}
}
func TestInvalidQueries(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		code       int
		wantError  bool
	}{
		{"unavailable", `secret`, 503, true},
		{"malformed", `{`, 200, true},
		{"missing result", `{"status":"success","data":{"resultType":"vector"}}`, 200, true},
		{"warning", `{"status":"success","warnings":["partial"],"data":{"resultType":"vector","result":[]}}`, 200, true},
		{"empty", `{"status":"success","data":{"resultType":"vector","result":[]}}`, 200, false},
		{"oversized", strings.Repeat("x", (1<<20)+1), 200, true},
		{"nonfinite", fmt.Sprintf(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"service":"payment-service"},"value":[%d,"NaN"]}]}}`, time.Now().Unix()), 200, false},
		{"stale", `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"service":"payment-service"},"value":[1,"1"]}]}}`, 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.code); _, _ = fmt.Fprint(w, tc.body) }))
			defer server.Close()
			client, err := New(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			got, err := client.Read(context.Background())
			if (err != nil) != tc.wantError {
				t.Fatalf("unexpected error %v", err)
			}
			if err == nil && got.Items[4].Status != "Unknown" {
				t.Fatal("missing data became healthy")
			}
		})
	}
}
func TestConfiguration(t *testing.T) {
	for _, url := range []string{"file:///tmp/x", "http://a:b@localhost", "http://localhost?query=up", "http://localhost#x"} {
		if _, err := New(url); err == nil {
			t.Fatalf("accepted %s", url)
		}
	}
	client, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Read(context.Background()); err == nil {
		t.Fatal("disabled telemetry returned success")
	}
}
