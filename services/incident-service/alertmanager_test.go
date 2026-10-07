package incident

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeIngester struct {
	seen []AlertObservation
	err  error
}

func (f *fakeIngester) IngestAlert(_ context.Context, source, environment, viewer string, alert AlertObservation) (Incident, bool, error) {
	if f.err != nil {
		return Incident{}, false, f.err
	}
	if _, _, err := alertCommand(source, environment, viewer, alert); err != nil {
		return Incident{}, false, err
	}
	f.seen = append(f.seen, alert)
	return Incident{ID: "inc", Environment: environment}, len(f.seen) > 1, nil
}

const webhookToken = "alert-webhook-token-0123456789abcdef"

func postNotification(t *testing.T, handler http.Handler, token, body string) (int, map[string]any) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/alertmanager", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var out map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %q: %v", recorder.Body.String(), err)
	}
	return recorder.Code, out
}

func TestAlertmanagerWebhookIngestsFiringAlerts(t *testing.T) {
	store := &fakeIngester{}
	handler := alertmanagerHandler(store, webhookToken, "local-alertmanager", "development", "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	started := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	body := `{"version":"4","status":"firing","receiver":"incident-ingest","alerts":[
		{"status":"firing","labels":{"alertname":"BusinessSuccessRateLow","environment":"staging","severity":"SEV-2"},"annotations":{"summary":"ignored"},"startsAt":"` + started + `","fingerprint":"a"},
		{"status":"resolved","labels":{"alertname":"ServiceUnavailable","service":"payment-service"},"startsAt":"` + started + `"},
		{"status":"firing","labels":{"alertname":"PostgresDown","severity":"SEV-1"},"startsAt":"` + started + `"},
		{"status":"firing","labels":{"alertname":"ServiceLatencyHigh","service":"unknown-service"},"startsAt":"` + started + `"}]}`
	code, out := postNotification(t, handler, webhookToken, body)
	if code != 200 || out["created"] != 1.0 || out["skipped"] != 3.0 || len(store.seen) != 1 {
		t.Fatalf("response %d %v seen %+v", code, out, store.seen)
	}
	if store.seen[0].State != "firing" || store.seen[0].ActiveAt.Format(time.RFC3339Nano) != started {
		t.Fatalf("observation %+v", store.seen[0])
	}
	// Alertmanager repeats the group; the episode resolves to the existing incident.
	if code, out = postNotification(t, handler, webhookToken, body); code != 200 || out["existing"] != 1.0 {
		t.Fatalf("repeat %d %v", code, out)
	}
}

func TestAlertmanagerWebhookRejectsUntrustedOrMalformedInput(t *testing.T) {
	handler := alertmanagerHandler(&fakeIngester{}, webhookToken, "local-alertmanager", "development", "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if code, _ := postNotification(t, handler, "wrong-token-0123456789abcdef0123456789", `{"version":"4","alerts":[]}`); code != 401 {
		t.Fatalf("unauthenticated status %d", code)
	}
	for _, body := range []string{`{"version":"3","alerts":[]}`, `{"version":"4"}`, `not-json`, `{"version":"4","alerts":[` + strings.Repeat(`{},`, 1000) + `{}]}`} {
		if code, _ := postNotification(t, handler, webhookToken, body); code != 400 {
			t.Fatalf("accepted %.20s with %d", body, code)
		}
	}
	if code, _ := postNotification(t, handler, webhookToken, strings.Repeat("x", (1<<20)+1)); code != 413 {
		t.Fatalf("oversized status %d", code)
	}
}

func TestAlertmanagerWebhookRetriesStorageFailure(t *testing.T) {
	handler := alertmanagerHandler(&fakeIngester{err: errors.New("database unavailable")}, webhookToken, "local-alertmanager", "development", "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	body := `{"version":"4","alerts":[{"status":"firing","labels":{"alertname":"ServiceUnavailable","service":"payment-service"},"startsAt":"` + time.Now().UTC().Format(time.RFC3339) + `"}]}`
	if code, _ := postNotification(t, handler, webhookToken, body); code != 503 {
		t.Fatalf("storage failure status %d", code)
	}
}

func TestAlertmanagerReceiverConfiguration(t *testing.T) {
	service := strings.Repeat("s", 32)
	operator := strings.Repeat("o", 32)
	for _, token := range []string{"short", service, operator} {
		if _, err := AlertmanagerReceiver("0.0.0.0:9094", token, service, operator, "local", "development", ""); err == nil {
			t.Fatalf("token %q accepted", token)
		}
	}
	if _, err := AlertmanagerReceiver("not-an-address", webhookToken, service, operator, "local", "development", ""); err == nil {
		t.Fatal("invalid address accepted")
	}
	if _, err := AlertmanagerReceiver("0.0.0.0:9094", webhookToken, service, operator, "local", "production", ""); err == nil {
		t.Fatal("unsupported environment accepted")
	}
	if _, err := AlertmanagerReceiver("0.0.0.0:9094", webhookToken, service, operator, "local", "development", "http://localhost:9090"); err != nil {
		t.Fatal(err)
	}
}
