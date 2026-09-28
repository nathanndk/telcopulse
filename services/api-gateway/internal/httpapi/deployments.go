package httpapi

import (
	"net/http"
	"net/url"
	"regexp"
)

var deploymentID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{7,99}$`)

func (s *Server) deploymentRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/deployments", s.deploymentRequest)
	mux.HandleFunc("GET /api/v1/deployments/operations", s.deploymentRequest)
	mux.HandleFunc("GET /api/v1/deployments/{id}", s.deploymentRequest)
}

func (s *Server) deploymentRequest(w http.ResponseWriter, r *http.Request) {
	if s.Deployments == nil {
		s.write(w, 503, map[string]string{"error": "deployment service is not configured"})
		return
	}
	path := "/internal/deployments"
	if r.URL.Path == "/api/v1/deployments/operations" {
		q := url.Values{}
		q.Set("environment", r.URL.Query().Get("environment"))
		path += "/operations?" + q.Encode()
	} else if id := r.PathValue("id"); id != "" {
		if !deploymentID.MatchString(id) {
			s.write(w, 404, map[string]string{"error": "deployment not found"})
			return
		}
		path += "/" + url.PathEscape(id)
	} else {
		q := url.Values{}
		for _, key := range []string{"environment", "service", "since", "until", "limit"} {
			if v := r.URL.Query().Get(key); v != "" {
				q.Set(key, v)
			}
		}
		path += "?" + q.Encode()
	}
	result, err := s.Deployments.Exchange(r.Context(), r.Method, path, nil, "")
	if err != nil {
		s.Log.Warn("deployment service request failed", "error", err)
		s.write(w, 503, map[string]string{"error": "deployment service unavailable"})
		return
	}
	switch result.Status {
	case 200, 404, 422:
		s.write(w, result.Status, result.Body)
	default:
		s.write(w, 503, map[string]string{"error": "deployment service unavailable"})
	}
}
