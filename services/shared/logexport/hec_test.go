package logexport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHECContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/services/collector/event" || r.Header.Get("Authorization") != "Splunk test-token" {
			t.Error("invalid request contract")
		}
		var body struct {
			Event      map[string]any `json:"event"`
			Index      string         `json:"index"`
			SourceType string         `json:"sourcetype"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Index != "telcopulse" || body.SourceType != "_json" || body.Event["error_code"] != "DB_TIMEOUT" || body.Event["trace_id"] != "trace" {
			t.Error("missing correlation fields")
		}
		_, _ = fmt.Fprint(w, `{"code":0,"text":"Success"}`)
	}))
	defer server.Close()
	client, err := NewHEC(server.URL+"/services/collector/event", "test-token", "telcopulse", true)
	if err != nil {
		t.Fatal(err)
	}
	if err = client.Send(context.Background(), []byte(`{"error_code":"DB_TIMEOUT","trace_id":"trace"}`)); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"null", "[]", "bad", strings.Repeat("x", 65537)} {
		if err = client.Send(context.Background(), []byte(input)); err == nil {
			t.Fatal("invalid event accepted")
		}
	}
}

func TestHECRejectsFailureAndRedirect(t *testing.T) {
	for _, body := range []string{`{}`, `{"code":4}`, `not-json`, strings.Repeat("x", 4097)} {
		t.Run(fmt.Sprint(len(body), body[:1]), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, body) }))
			defer server.Close()
			client, _ := NewHEC(server.URL+"/services/collector/event", "secret-token", "", true)
			if err := client.Send(context.Background(), []byte(`{}`)); err == nil || strings.Contains(err.Error(), "secret-token") {
				t.Fatal("failure missing or leaks token")
			}
		})
	}
	reached := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	client, _ := NewHEC(redirect.URL+"/services/collector/event", "secret-token", "", true)
	if err := client.Send(context.Background(), []byte(`{}`)); err == nil || reached {
		t.Fatal("redirect followed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.Send(ctx, []byte(`{}`)); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestHECConfiguration(t *testing.T) {
	for _, endpoint := range []string{"http://example.com/services/collector/event", "https://user:password@example.com/services/collector/event", "https://example.com/services/collector/event?token=x", "https://example.com/wrong", "file:///services/collector/event"} {
		if _, err := NewHEC(endpoint, "token", "", true); err == nil {
			t.Fatalf("accepted %s", endpoint)
		}
	}
	if _, err := NewHEC("http://127.0.0.1/services/collector/event", "token", "", false); err == nil {
		t.Fatal("plaintext accepted outside local mode")
	}
	if _, err := NewHEC("https://example.com/services/collector/event", "token", "telcopulse", false); err != nil {
		t.Fatal(err)
	}
}
