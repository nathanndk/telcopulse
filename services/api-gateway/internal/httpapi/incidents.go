package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"telcopulse/services/shared/runtime"
)

var incidentID = regexp.MustCompile(`^INC-[a-f0-9]{24}$`)

func (s *Server) incidentRoutes(mux *http.ServeMux) {
	for _, pattern := range []string{"GET /api/v1/incidents/overview", "GET /api/v1/incidents/operations", "GET /api/v1/incidents/actions", "GET /api/v1/incidents/postmortems", "GET /api/v1/incidents/saved-views", "POST /api/v1/incidents/saved-views", "PUT /api/v1/incidents/saved-views/{viewID}", "DELETE /api/v1/incidents/saved-views/{viewID}", "GET /api/v1/incidents", "POST /api/v1/incidents", "GET /api/v1/incidents/{id}", "GET /api/v1/incidents/{id}/logs", "GET /api/v1/incidents/{id}/traces", "GET /api/v1/incidents/{id}/metrics", "GET /api/v1/incidents/{id}/infrastructure", "GET /api/v1/incidents/{id}/purchase-impact", "GET /api/v1/incidents/{id}/recovery-assessment", "PUT /api/v1/incidents/{id}", "POST /api/v1/incidents/{id}/escalations", "POST /api/v1/incidents/{id}/postmortem"} {
		mux.HandleFunc(pattern, s.incidentRequest)
	}
}
func (s *Server) incidentRequest(w http.ResponseWriter, r *http.Request) {
	if s.Incidents == nil {
		s.write(w, 503, map[string]string{"error": "incident service is not configured"})
		return
	}
	path := "/internal/incidents"
	if r.URL.Path == "/api/v1/incidents/saved-views" {
		path += "/saved-views"
		if r.Method == http.MethodGet {
			path += "?" + url.Values{"environment": []string{r.URL.Query().Get("environment")}}.Encode()
		}
	} else if viewID := r.PathValue("viewID"); viewID != "" {
		if !regexp.MustCompile(`^VIEW-[a-f0-9]{24}$`).MatchString(viewID) {
			s.write(w, 404, map[string]string{"error": "saved view not found"})
			return
		}
		path += "/saved-views/" + viewID
	} else if r.URL.Path == "/api/v1/incidents/postmortems" {
		query := url.Values{}
		for _, key := range []string{"environment", "service", "severity", "search", "limit", "cursor"} {
			if value := r.URL.Query().Get(key); value != "" {
				query.Set(key, value)
			}
		}
		path += "/postmortems?" + query.Encode()
	} else if r.URL.Path == "/api/v1/incidents/actions" {
		query := url.Values{}
		for _, key := range []string{"environment", "status", "priority", "owner", "search", "limit", "cursor"} {
			if value := r.URL.Query().Get(key); value != "" {
				query.Set(key, value)
			}
		}
		path += "/actions?" + query.Encode()
	} else if r.URL.Path == "/api/v1/incidents/overview" || r.URL.Path == "/api/v1/incidents/operations" {
		query := url.Values{}
		query.Set("environment", r.URL.Query().Get("environment"))
		if r.URL.Path == "/api/v1/incidents/overview" {
			path += "/overview?" + query.Encode()
		} else {
			path += "/operations?" + query.Encode()
		}
	} else if id := r.PathValue("id"); id != "" {
		if !incidentID.MatchString(id) {
			s.write(w, 404, map[string]string{"error": "incident not found"})
			return
		}
		path += "/" + id
		if r.Method == http.MethodGet && (strings.HasSuffix(r.URL.Path, "/logs") || strings.HasSuffix(r.URL.Path, "/traces") || strings.HasSuffix(r.URL.Path, "/metrics") || strings.HasSuffix(r.URL.Path, "/infrastructure") || strings.HasSuffix(r.URL.Path, "/purchase-impact") || strings.HasSuffix(r.URL.Path, "/recovery-assessment")) {
			q := url.Values{}
			if value := r.URL.Query().Get("window"); value != "" {
				q.Set("window", value)
			}
			if strings.HasSuffix(r.URL.Path, "/purchase-impact") {
				if value := r.URL.Query().Get("cursor"); value != "" {
					q.Set("cursor", value)
				}
				path += "/purchase-impact?" + q.Encode()
			} else if strings.HasSuffix(r.URL.Path, "/recovery-assessment") {
				path += "/recovery-assessment"
			} else if strings.HasSuffix(r.URL.Path, "/traces") {
				path += "/traces?" + q.Encode()
			} else if strings.HasSuffix(r.URL.Path, "/metrics") {
				path += "/metrics?" + q.Encode()
			} else if strings.HasSuffix(r.URL.Path, "/infrastructure") {
				path += "/infrastructure?" + q.Encode()
			} else {
				path += "/logs?" + q.Encode()
			}
		} else if r.Method == http.MethodGet {
			q := url.Values{}
			if value := r.URL.Query().Get("history_after"); value != "" {
				q.Set("history_after", value)
			}
			path += "?" + q.Encode()
		} else if r.Method == http.MethodPost {
			if strings.HasSuffix(r.URL.Path, "/postmortem") {
				path += "/postmortem"
			} else {
				path += "/escalations"
			}
		}
	} else if r.Method == http.MethodGet {
		query := url.Values{}
		for _, key := range []string{"environment", "state", "severity", "service", "owner", "search", "since", "sort", "limit", "cursor"} {
			if value := r.URL.Query().Get(key); value != "" {
				query.Set(key, value)
			}
		}
		path += "?" + query.Encode()
	}
	var input any
	key := ""
	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		if strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]) != "application/json" {
			s.write(w, 415, map[string]string{"error": "use application/json"})
			return
		}
		if r.Method == http.MethodPost && r.PathValue("id") == "" && r.URL.Path != "/api/v1/incidents/saved-views" {
			key = r.Header.Get("Idempotency-Key")
			if !keyPattern.MatchString(key) {
				s.write(w, 400, map[string]string{"error": "a valid Idempotency-Key is required"})
				return
			}
		}
		var raw json.RawMessage
		if !runtime.DecodeLimit(w, r, &raw, 128<<10) {
			return
		}
		input = raw
	}
	actor, role := "", ""
	if user := operatorFrom(r.Context()); user.ID != "" {
		actor = "operator:" + user.ID
		role = user.Role
	}
	result, err := s.Incidents.ExchangeAs(r.Context(), r.Method, path, input, key, actor, role)
	if err != nil {
		s.Log.Warn("incident service request failed", "error", err)
		message := "incident service unavailable"
		if r.Method == http.MethodPost && r.PathValue("id") == "" && r.URL.Path != "/api/v1/incidents/saved-views" {
			message += "; creation retries must reuse the same key"
		}
		s.write(w, 503, map[string]string{"error": message})
		return
	}
	switch result.Status {
	case http.StatusOK, http.StatusCreated, http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity:
		s.write(w, result.Status, result.Body)
	default:
		message := "incident service unavailable"
		if strings.HasSuffix(r.URL.Path, "/logs") {
			message = "log search unavailable"
		} else if strings.HasSuffix(r.URL.Path, "/traces") {
			message = "trace search unavailable"
		} else if strings.HasSuffix(r.URL.Path, "/metrics") {
			message = "metric query unavailable"
		} else if strings.HasSuffix(r.URL.Path, "/infrastructure") {
			message = "infrastructure query unavailable"
		} else if strings.HasSuffix(r.URL.Path, "/purchase-impact") {
			message = "purchase impact query unavailable"
		} else if strings.HasSuffix(r.URL.Path, "/recovery-assessment") {
			message = "recovery assessment unavailable"
		}
		s.write(w, 503, map[string]string{"error": message})
	}
}
