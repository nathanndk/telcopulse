package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"telcopulse/services/shared/runtime"
)

var simulationID = regexp.MustCompile(`^SIM-[a-f0-9]{24}$`)

func (s *Server) simulationRoutes(mux *http.ServeMux) {
	for _, pattern := range []string{"GET /api/v1/simulations", "GET /api/v1/simulations/{id}", "POST /api/v1/simulations", "POST /api/v1/simulations/{id}/stop"} {
		mux.HandleFunc(pattern, s.simulationRequest)
	}
}
func (s *Server) simulationRequest(w http.ResponseWriter, r *http.Request) {
	if s.Simulations == nil {
		s.write(w, 503, map[string]string{"error": "simulation service not configured"})
		return
	}
	path := "/internal/simulations"
	var input any
	key := ""
	if id := r.PathValue("id"); id != "" {
		if !simulationID.MatchString(id) {
			s.write(w, 404, map[string]string{"error": "simulation not found"})
			return
		}
		path += "/" + id
		if r.Method == http.MethodPost {
			path += "/stop"
		}
	}
	if r.Method == http.MethodGet {
		if r.PathValue("id") != "" {
			path += "?" + url.Values{"cursor": {r.URL.Query().Get("cursor")}}.Encode()
		} else {
			environment, ok := s.environment(w, r)
			if !ok {
				return
			}
			path += "?" + url.Values{"environment": {environment}}.Encode()
		}
	} else {
		if strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]) != "application/json" {
			s.write(w, 415, map[string]string{"error": "use application/json"})
			return
		}
		if r.PathValue("id") == "" {
			key = r.Header.Get("Idempotency-Key")
			if !keyPattern.MatchString(key) {
				s.write(w, 400, map[string]string{"error": "a valid Idempotency-Key is required"})
				return
			}
		}
		var raw json.RawMessage
		if !runtime.DecodeLimit(w, r, &raw, 8192) {
			return
		}
		input = raw
	}
	actor, role := "", ""
	if user := operatorFrom(r.Context()); user.ID != "" {
		actor = "operator:" + user.ID
		role = user.Role
	}
	result, err := s.Simulations.ExchangeAs(r.Context(), r.Method, path, input, key, actor, role)
	if err != nil {
		s.write(w, 503, map[string]string{"error": "simulation service unavailable; retry creation with the same key"})
		return
	}
	switch result.Status {
	case 200, 201, 400, 403, 404, 409, 422:
		s.write(w, result.Status, result.Body)
	default:
		s.write(w, 503, map[string]string{"error": "simulation service unavailable"})
	}
}
