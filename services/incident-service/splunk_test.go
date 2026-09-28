package incident

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"telcopulse/services/shared/domain"
)

type fakeLogSearcher func(context.Context, Incident, time.Time, time.Time) ([]LogEvent, error)

func (f fakeLogSearcher) Search(ctx context.Context, incident Incident, start, end time.Time) ([]LogEvent, error) {
	return f(ctx, incident, start, end)
}

func TestIncidentLogEndpointScopesPersistedIncident(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL required for log endpoint integration")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store := Store{Pool: pool}
	key, err := domain.NewID(12)
	if err != nil {
		t.Fatal(err)
	}
	created, _, err := store.Create(ctx, Create{Fields: Fields{Title: "Log search integration", Severity: "SEV-3", Owner: "test"}, Environment: "development", Service: "payment-service"}, "log-search-"+key, "integration-test")
	if err != nil {
		t.Fatal(err)
	}
	called := false
	searcher := fakeLogSearcher(func(_ context.Context, incident Incident, start, end time.Time) ([]LogEvent, error) {
		called = true
		if incident.ID != created.ID || incident.Service != "payment-service" || incident.Environment != "development" || !start.Equal(created.DetectedAt.Add(-15*time.Minute)) || end.Sub(created.DetectedAt) > 2*time.Hour {
			t.Errorf("unsafe endpoint search scope: %+v %s %s", incident, start, end)
		}
		return []LogEvent{{At: "2026-09-28 01:00:00 UTC", Level: "ERROR", Service: "payment-service", Message: "Database timeout", ErrorCode: "DB_TIMEOUT"}}, nil
	})
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := HandlerWithLogs(pool, log, "test-mutation-token", searcher)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/internal/incidents/"+created.ID+"/logs?window=2h", nil))
	if w.Code != 200 || !called || !strings.Contains(w.Body.String(), `"error_code":"DB_TIMEOUT"`) {
		t.Fatalf("configured endpoint: %d %s", w.Code, w.Body.String())
	}
	unconfigured := HandlerWithLogs(pool, log, "test-mutation-token", nil)
	w = httptest.NewRecorder()
	unconfigured.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/internal/incidents/"+created.ID+"/logs", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"configured":false`) {
		t.Fatalf("unconfigured endpoint: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/internal/incidents/"+created.ID+"/logs?window=forever", nil))
	if w.Code != 422 {
		t.Fatalf("accepted invalid window: %d %s", w.Code, w.Body.String())
	}
}

func TestSplunkSearchScopesAndSanitizesResults(t *testing.T) {
	start := time.Date(2026, 9, 27, 1, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/services/search/v2/jobs" || r.Header.Get("Authorization") != "Bearer test-search-token" {
			t.Errorf("unexpected search request: %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		for key, want := range map[string]string{"exec_mode": "oneshot", "output_mode": "json", "earliest_time": "1790470800", "latest_time": "1790478000"} {
			if r.Form.Get(key) != want {
				t.Errorf("%s: got %q want %q", key, r.Form.Get(key), want)
			}
		}
		search := r.Form.Get("search")
		if !strings.Contains(search, `index=telcopulse`) || !strings.Contains(search, `environment="development"`) || !strings.Contains(search, `service="payment-service"`) || !strings.Contains(search, "| head 50") || strings.Contains(search, "secret") {
			t.Errorf("unexpected search scope: %s", search)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[{"_time":"2026-09-27 01:30:00 UTC","level":"ERROR","environment":"development","service":"payment-service","msg":"Database timeout for 628123456789","trace_id":"abc","transaction_id":"txn-1","error_code":"DB_TIMEOUT","msisdn":"628123456789"},{"level":"ERROR","environment":"staging","service":"payment-service","msg":"Other environment"},{"level":"WARN","environment":"development","service":"package-service","msg":"Other service"},{"level":"DEBUG","environment":"development","service":"payment-service","msg":"Unapproved level"}]}`)
	}))
	defer server.Close()
	client, err := NewSplunkSearch(server.URL+"/services/search/v2/jobs", "test-search-token", "telcopulse", true)
	if err != nil {
		t.Fatal(err)
	}
	items, err := client.Search(context.Background(), Incident{Environment: "development", Service: "payment-service"}, start, end)
	if err != nil || len(items) != 1 || items[0].ErrorCode != "DB_TIMEOUT" || items[0].Message != "Database timeout for 62812*****789" {
		t.Fatalf("scoped search: %+v %v", items, err)
	}
	if strings.Contains(items[0].Message, "628123456789") {
		t.Fatal("returned unselected sensitive field")
	}
	if _, err := client.Search(context.Background(), Incident{Environment: "development", Service: `payment-service" | delete`}, start, end); !errors.Is(err, ErrInvalid) {
		t.Fatal("accepted service text that could change SPL")
	}
}

func TestSplunkSearchConfigAndFailureBounds(t *testing.T) {
	for _, endpoint := range []string{"http://remote.example/services/search/v2/jobs", "https://user:password@example.com/services/search/v2/jobs", "https://example.com/services/search/v2/jobs?x=1", "https://example.com/services/search/jobs"} {
		if _, err := NewSplunkSearch(endpoint, "test-search-token", "telcopulse", true); err == nil {
			t.Fatalf("accepted endpoint %s", endpoint)
		}
	}
	if _, err := NewSplunkSearch("https://example.com/services/search/v2/jobs", "test-search-token", `main | delete`, false); err == nil {
		t.Fatal("accepted unsafe index")
	}
	for _, result := range []struct {
		status int
		body   string
	}{
		{503, `vendor secret detail`},
		{200, `{"not_results":[]}`},
		{200, strings.Repeat("x", (256<<10)+1)},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(result.status)
			_, _ = io.WriteString(w, result.body)
		}))
		client, _ := NewSplunkSearch(server.URL+"/services/search/v2/jobs", "test-search-token", "telcopulse", true)
		_, err := client.Search(context.Background(), Incident{Environment: "development", Service: "payment-service"}, time.Now().Add(-time.Hour), time.Now())
		server.Close()
		if err == nil || strings.Contains(err.Error(), "vendor secret") {
			t.Fatalf("unsafe failure: %v", err)
		}
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.com", http.StatusFound)
	}))
	defer redirect.Close()
	client, _ := NewSplunkSearch(redirect.URL+"/services/search/v2/jobs", "test-search-token", "telcopulse", true)
	if _, err := client.Search(context.Background(), Incident{Environment: "development", Service: "payment-service"}, time.Now().Add(-time.Hour), time.Now()); err == nil {
		t.Fatal("followed search redirect")
	}
}

func TestSplunkSearchConfigFileAndWindow(t *testing.T) {
	t.Setenv("SPLUNK_SEARCH_URL", "")
	t.Setenv("SPLUNK_SEARCH_TOKEN_FILE", "")
	client, err := SplunkSearchFromEnv()
	if err != nil || client != nil {
		t.Fatalf("unconfigured search: %v", err)
	}
	t.Setenv("SPLUNK_SEARCH_URL", "https://example.com/services/search/v2/jobs")
	if _, err := SplunkSearchFromEnv(); err == nil {
		t.Fatal("accepted incomplete configuration")
	}
	file, err := os.CreateTemp(t.TempDir(), "token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("test-search-token\n"); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	t.Setenv("SPLUNK_SEARCH_TOKEN_FILE", file.Name())
	client, err = SplunkSearchFromEnv()
	if err != nil || client == nil {
		t.Fatalf("token file configuration: %v", err)
	}
	detected := time.Date(2026, 9, 28, 1, 0, 0, 0, time.UTC)
	start, end, err := logWindow(Incident{DetectedAt: detected}, "2h", detected.Add(time.Hour))
	if err != nil || !start.Equal(detected.Add(-15*time.Minute)) || !end.Equal(detected.Add(time.Hour)) {
		t.Fatalf("bounded window: %s %s %v", start, end, err)
	}
	if _, _, err := logWindow(Incident{DetectedAt: detected}, "one-year", detected.Add(time.Hour)); !errors.Is(err, ErrInvalid) {
		t.Fatal("accepted unbounded window")
	}
}
