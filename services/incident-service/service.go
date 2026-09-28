package incident

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"telcopulse/services/shared/runtime"
	"time"
)

// Handler exposes the service-authenticated incident API.
var verifiedActor = regexp.MustCompile(`^operator:USR-[a-f0-9]{24}$`)

func operatorIdentity(r *http.Request) (actor, role string, valid bool) {
	actor = r.Header.Get("X-Operator-Actor")
	role = r.Header.Get("X-Operator-Role")
	if actor == "" && role == "" {
		return "local-operator", "", true
	}
	if !verifiedActor.MatchString(actor) || !slices.Contains([]string{"Viewer", "Operator", "Engineer", "Incident Commander", "Administrator"}, role) {
		return "", "", false
	}
	return actor, role, true
}

func Handler(pool *pgxpool.Pool, log *slog.Logger, mutationToken string) http.Handler {
	return HandlerWithEvidence(pool, log, mutationToken, nil, nil)
}

func HandlerWithLogs(pool *pgxpool.Pool, log *slog.Logger, mutationToken string, searcher LogSearcher) http.Handler {
	return HandlerWithEvidence(pool, log, mutationToken, searcher, nil)
}

func HandlerWithEvidence(pool *pgxpool.Pool, log *slog.Logger, mutationToken string, searcher LogSearcher, traces TraceSearcher) http.Handler {
	return HandlerWithSources(pool, log, mutationToken, searcher, traces, nil)
}

func HandlerWithSources(pool *pgxpool.Pool, log *slog.Logger, mutationToken string, searcher LogSearcher, traces TraceSearcher, metrics MetricSearcher) http.Handler {
	return HandlerWithAllSources(pool, log, mutationToken, searcher, traces, metrics, nil)
}

func HandlerWithAllSources(pool *pgxpool.Pool, log *slog.Logger, mutationToken string, searcher LogSearcher, traces TraceSearcher, metrics MetricSearcher, infrastructure InfrastructureSearcher) http.Handler {
	store := Store{Pool: pool}
	mux := http.NewServeMux()
	fail := func(w http.ResponseWriter, err error) {
		switch {
		case errors.Is(err, ErrForbidden):
			runtime.JSON(w, 403, map[string]string{"error": "role cannot perform this action"})
		case errors.Is(err, ErrInvalid):
			runtime.JSON(w, 422, map[string]string{"error": err.Error()})
		case errors.Is(err, ErrConflict):
			runtime.JSON(w, 409, map[string]string{"error": ErrConflict.Error()})
		case errors.Is(err, ErrSavedViewConflict):
			runtime.JSON(w, 409, map[string]string{"error": ErrSavedViewConflict.Error()})
		case errors.Is(err, ErrNotFound):
			runtime.JSON(w, 404, map[string]string{"error": ErrNotFound.Error()})
		default:
			log.Error("incident persistence failed", "error", err)
			runtime.JSON(w, 503, map[string]string{"error": "incident service unavailable"})
		}
	}
	mux.HandleFunc("GET /internal/incidents/overview", func(w http.ResponseWriter, r *http.Request) {
		result, err := store.Overview(r.Context(), r.URL.Query().Get("environment"))
		if err != nil {
			fail(w, err)
			return
		}
		runtime.JSON(w, 200, result)
	})
	mux.HandleFunc("GET /internal/incidents/operations", func(w http.ResponseWriter, r *http.Request) {
		result, err := store.Operations(r.Context(), r.URL.Query().Get("environment"))
		if err != nil {
			fail(w, err)
			return
		}
		runtime.JSON(w, 200, result)
	})
	mux.HandleFunc("GET /internal/incidents/actions", func(w http.ResponseWriter, r *http.Request) {
		limit := 25
		if raw := r.URL.Query().Get("limit"); raw != "" {
			var err error
			limit, err = strconv.Atoi(raw)
			if err != nil {
				runtime.JSON(w, 422, map[string]string{"error": "invalid limit"})
				return
			}
		}
		status := r.URL.Query().Get("status")
		if status == "" {
			status = "active"
		}
		result, err := store.Actions(r.Context(), ActionFilter{Environment: r.URL.Query().Get("environment"),
			Status: status, Priority: r.URL.Query().Get("priority"), Owner: r.URL.Query().Get("owner"),
			Search: r.URL.Query().Get("search"), Limit: limit, Cursor: r.URL.Query().Get("cursor")})
		if err != nil {
			fail(w, err)
			return
		}
		runtime.JSON(w, 200, result)
	})
	mux.HandleFunc("GET /internal/incidents/postmortems", func(w http.ResponseWriter, r *http.Request) {
		limit := 20
		if raw := r.URL.Query().Get("limit"); raw != "" {
			var err error
			limit, err = strconv.Atoi(raw)
			if err != nil {
				runtime.JSON(w, 422, map[string]string{"error": "invalid limit"})
				return
			}
		}
		result, err := store.Postmortems(r.Context(), ReportFilter{Environment: r.URL.Query().Get("environment"),
			Service: r.URL.Query().Get("service"), Severity: r.URL.Query().Get("severity"),
			Search: r.URL.Query().Get("search"), Limit: limit, Cursor: r.URL.Query().Get("cursor")})
		if err != nil {
			fail(w, err)
			return
		}
		runtime.JSON(w, 200, result)
	})
	mux.HandleFunc("GET /internal/incidents/saved-views", func(w http.ResponseWriter, r *http.Request) {
		actor, _, valid := operatorIdentity(r)
		if !valid {
			fail(w, ErrForbidden)
			return
		}
		result, err := store.ListSavedViews(r.Context(), actor, r.URL.Query().Get("environment"))
		if err != nil {
			fail(w, err)
			return
		}
		runtime.JSON(w, 200, result)
	})
	mux.HandleFunc("POST /internal/incidents/saved-views", func(w http.ResponseWriter, r *http.Request) {
		actor, _, valid := operatorIdentity(r)
		if !valid {
			fail(w, ErrForbidden)
			return
		}
		var input SavedViewInput
		if !runtime.DecodeLimit(w, r, &input, 4096) {
			return
		}
		result, err := store.CreateSavedView(r.Context(), actor, input)
		if err != nil {
			fail(w, err)
			return
		}
		runtime.JSON(w, 201, result)
	})
	mux.HandleFunc("PUT /internal/incidents/saved-views/{viewID}", func(w http.ResponseWriter, r *http.Request) {
		actor, _, valid := operatorIdentity(r)
		if !valid {
			fail(w, ErrForbidden)
			return
		}
		var input SavedViewInput
		if !runtime.DecodeLimit(w, r, &input, 4096) {
			return
		}
		result, err := store.UpdateSavedView(r.Context(), actor, r.PathValue("viewID"), input)
		if err != nil {
			fail(w, err)
			return
		}
		runtime.JSON(w, 200, result)
	})
	mux.HandleFunc("DELETE /internal/incidents/saved-views/{viewID}", func(w http.ResponseWriter, r *http.Request) {
		actor, _, valid := operatorIdentity(r)
		if !valid {
			fail(w, ErrForbidden)
			return
		}
		if err := store.DeleteSavedView(r.Context(), actor, r.PathValue("viewID")); err != nil {
			fail(w, err)
			return
		}
		runtime.JSON(w, 200, map[string]bool{"deleted": true})
	})
	mux.HandleFunc("GET /internal/incidents/{id}/logs", func(w http.ResponseWriter, r *http.Request) {
		if !incidentIDPattern.MatchString(r.PathValue("id")) {
			fail(w, ErrNotFound)
			return
		}
		detail, err := store.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		start, end, err := logWindow(detail.Incident, r.URL.Query().Get("window"), time.Now().UTC())
		if err != nil {
			fail(w, err)
			return
		}
		result := LogEvidence{Configured: searcher != nil, Source: "splunk", Start: start, End: end, Items: []LogEvent{}}
		if searcher != nil {
			result.Items, err = searcher.Search(r.Context(), detail.Incident, start, end)
			if err != nil {
				fail(w, err)
				return
			}
		}
		runtime.JSON(w, 200, result)
	})
	mux.HandleFunc("GET /internal/incidents/{id}/traces", func(w http.ResponseWriter, r *http.Request) {
		if !incidentIDPattern.MatchString(r.PathValue("id")) {
			fail(w, ErrNotFound)
			return
		}
		detail, err := store.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		start, end, err := logWindow(detail.Incident, r.URL.Query().Get("window"), time.Now().UTC())
		if err != nil {
			fail(w, err)
			return
		}
		result := TraceEvidence{Configured: traces != nil, Source: "jaeger", Start: start, End: end, Items: []TraceRecord{}}
		if traces != nil {
			result.Items, err = traces.SearchTraces(r.Context(), detail.Incident, start, end)
			if err != nil {
				fail(w, err)
				return
			}
		}
		runtime.JSON(w, 200, result)
	})
	mux.HandleFunc("GET /internal/incidents/{id}/metrics", func(w http.ResponseWriter, r *http.Request) {
		if !incidentIDPattern.MatchString(r.PathValue("id")) {
			fail(w, ErrNotFound)
			return
		}
		detail, err := store.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		start, end, err := logWindow(detail.Incident, r.URL.Query().Get("window"), time.Now().UTC())
		if err != nil {
			fail(w, err)
			return
		}
		result := MetricEvidence{Configured: metrics != nil, Source: "prometheus", Start: start, End: end, Series: []MetricSeries{}}
		if metrics != nil {
			result.Series, result.Step, err = metrics.SearchMetrics(r.Context(), detail.Incident, start, end)
			if err != nil {
				fail(w, err)
				return
			}
		}
		runtime.JSON(w, 200, result)
	})
	mux.HandleFunc("GET /internal/incidents/{id}/infrastructure", func(w http.ResponseWriter, r *http.Request) {
		if !incidentIDPattern.MatchString(r.PathValue("id")) {
			fail(w, ErrNotFound)
			return
		}
		detail, err := store.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		start, end, err := logWindow(detail.Incident, r.URL.Query().Get("window"), time.Now().UTC())
		if err != nil {
			fail(w, err)
			return
		}
		result := InfrastructureEvidence{Configured: infrastructure != nil, Source: "datadog", Start: start, End: end, Series: []InfrastructureSeries{}}
		if infrastructure != nil {
			result.Series, result.Limited, err = infrastructure.SearchInfrastructure(r.Context(), detail.Incident, start, end)
			if err != nil {
				fail(w, err)
				return
			}
		}
		runtime.JSON(w, 200, result)
	})
	mux.HandleFunc("GET /internal/incidents/{id}/purchase-impact", func(w http.ResponseWriter, r *http.Request) {
		if !incidentIDPattern.MatchString(r.PathValue("id")) {
			fail(w, ErrNotFound)
			return
		}
		item, err := store.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		result, err := store.PurchaseImpact(ctx, item.Incident, r.URL.Query().Get("window"), r.URL.Query().Get("cursor"))
		if err != nil {
			fail(w, err)
			return
		}
		runtime.JSON(w, 200, result)
	})
	mux.HandleFunc("GET /internal/incidents/{id}/recovery-assessment", func(w http.ResponseWriter, r *http.Request) {
		if !incidentIDPattern.MatchString(r.PathValue("id")) {
			fail(w, ErrNotFound)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		result, err := store.AssessRecovery(ctx, r.PathValue("id"), time.Now().UTC())
		if err != nil {
			fail(w, err)
			return
		}
		runtime.JSON(w, 200, result)
	})
	mux.HandleFunc("POST /internal/incidents", func(w http.ResponseWriter, r *http.Request) {
		actor, role, valid := operatorIdentity(r)
		if !valid || (role != "" && !slices.Contains([]string{"Operator", "Incident Commander", "Administrator"}, role)) {
			fail(w, ErrForbidden)
			return
		}
		var input Create
		if !runtime.DecodeLimit(w, r, &input, 128<<10) {
			return
		}
		result, replay, err := store.Create(r.Context(), input, r.Header.Get("Idempotency-Key"), actor)
		if err != nil {
			fail(w, err)
			return
		}
		status := http.StatusCreated
		if replay {
			status = http.StatusOK
		}
		runtime.JSON(w, status, result)
	})
	mux.HandleFunc("PUT /internal/incidents/{id}", func(w http.ResponseWriter, r *http.Request) {
		actor, role, valid := operatorIdentity(r)
		if !valid {
			fail(w, ErrForbidden)
			return
		}
		var input Update
		if !runtime.DecodeLimit(w, r, &input, 128<<10) {
			return
		}
		result, err := store.UpdateAs(r.Context(), r.PathValue("id"), actor, role, input)
		if err != nil {
			fail(w, err)
			return
		}
		runtime.JSON(w, 200, result)
	})
	mux.HandleFunc("POST /internal/incidents/{id}/escalations", func(w http.ResponseWriter, r *http.Request) {
		actor, role, valid := operatorIdentity(r)
		if !valid {
			fail(w, ErrForbidden)
			return
		}
		var input Escalate
		if !runtime.DecodeLimit(w, r, &input, 4096) {
			return
		}
		result, err := store.EscalateAs(r.Context(), r.PathValue("id"), actor, role, input)
		if err != nil {
			fail(w, err)
			return
		}
		runtime.JSON(w, 200, result)
	})
	mux.HandleFunc("POST /internal/incidents/{id}/postmortem", func(w http.ResponseWriter, r *http.Request) {
		actor, role, valid := operatorIdentity(r)
		if !valid {
			fail(w, ErrForbidden)
			return
		}
		var input GeneratePostmortem
		if !runtime.DecodeLimit(w, r, &input, 32<<10) {
			return
		}
		report, err := store.GeneratePostmortemAs(r.Context(), r.PathValue("id"), actor, role, input)
		if err != nil {
			fail(w, err)
			return
		}
		runtime.JSON(w, http.StatusCreated, report)
	})
	mux.HandleFunc("GET /internal/incidents/{id}", func(w http.ResponseWriter, r *http.Request) {
		var after int64
		if raw := r.URL.Query().Get("history_after"); raw != "" {
			var err error
			after, err = strconv.ParseInt(raw, 10, 64)
			if err != nil || after < 0 {
				runtime.JSON(w, 422, map[string]string{"error": "invalid history cursor"})
				return
			}
		}
		result, err := store.GetHistory(r.Context(), r.PathValue("id"), after)
		if err != nil {
			fail(w, err)
			return
		}
		runtime.JSON(w, 200, result)
	})
	mux.HandleFunc("GET /internal/incidents", func(w http.ResponseWriter, r *http.Request) {
		limit := 50
		if raw := r.URL.Query().Get("limit"); raw != "" {
			var err error
			limit, err = strconv.Atoi(raw)
			if err != nil {
				runtime.JSON(w, 422, map[string]string{"error": "invalid limit"})
				return
			}
		}
		result, err := store.ListFiltered(r.Context(), ListFilter{Environment: r.URL.Query().Get("environment"), State: State(r.URL.Query().Get("state")),
			Severity: r.URL.Query().Get("severity"), Service: r.URL.Query().Get("service"), Owner: r.URL.Query().Get("owner"),
			Search: r.URL.Query().Get("search"), Since: r.URL.Query().Get("since"), Sort: r.URL.Query().Get("sort"), Limit: limit, Cursor: r.URL.Query().Get("cursor")})
		if err != nil {
			fail(w, err)
			return
		}
		runtime.JSON(w, 200, result)
	})
	return runtime.RequireMutationToken(mux, mutationToken, func(r *http.Request) bool {
		return (r.Method == http.MethodPost && r.URL.Path == "/internal/incidents") ||
			((r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete) && strings.HasPrefix(r.URL.Path, "/internal/incidents/saved-views")) ||
			(r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/internal/incidents/")) ||
			(r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/internal/incidents/") && (strings.HasSuffix(r.URL.Path, "/escalations") || strings.HasSuffix(r.URL.Path, "/postmortem")))
	})
}
