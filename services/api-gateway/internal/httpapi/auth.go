package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"telcopulse/services/shared/rpc"
)

const sessionCookie = "telcopulse_session"

var operatorID = regexp.MustCompile(`^USR-[a-f0-9]{24}$`)

type Operator struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

type LoginSession struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	User      Operator  `json:"user"`
}

type Authenticator interface {
	Login(context.Context, string, string) (LoginSession, error)
	Validate(context.Context, string) (Operator, error)
	Logout(context.Context, string) error
}

// RemoteAuth exchanges server-only credentials with auth-service.
type RemoteAuth struct{ Client *rpc.Client }

var errUnauthenticated = errors.New("unauthenticated")

func (a RemoteAuth) Login(ctx context.Context, username, password string) (LoginSession, error) {
	var session LoginSession
	result, err := a.Client.Exchange(ctx, http.MethodPost, "/internal/auth/login", map[string]string{"username": username, "password": password}, "")
	if err != nil {
		return session, err
	}
	if result.Status == http.StatusUnauthorized {
		return session, errUnauthenticated
	}
	if result.Status != http.StatusOK {
		return session, errors.New("authentication service unavailable")
	}
	err = json.Unmarshal(result.Body, &session)
	if err != nil || !validOperator(session.User) || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(session.Token) || !session.ExpiresAt.After(time.Now()) {
		return LoginSession{}, errors.New("invalid authentication service response")
	}
	return session, nil
}

func (a RemoteAuth) Validate(ctx context.Context, token string) (Operator, error) {
	var user Operator
	result, err := a.Client.Exchange(ctx, http.MethodPost, "/internal/auth/validate", map[string]string{"token": token}, "")
	if err != nil {
		return user, err
	}
	if result.Status == http.StatusUnauthorized {
		return user, errUnauthenticated
	}
	if result.Status != http.StatusOK {
		return user, errors.New("authentication service unavailable")
	}
	err = json.Unmarshal(result.Body, &user)
	if err != nil || !validOperator(user) {
		return Operator{}, errors.New("invalid authentication service response")
	}
	return user, nil
}

func (a RemoteAuth) Logout(ctx context.Context, token string) error {
	result, err := a.Client.Exchange(ctx, http.MethodPost, "/internal/auth/logout", map[string]string{"token": token}, "")
	if err != nil {
		return err
	}
	if result.Status != http.StatusOK {
		return errors.New("authentication service unavailable")
	}
	return nil
}

// Management sends the browser session to auth-service for an independent
// administrator check; it never trusts a browser-supplied role claim.
func (a RemoteAuth) Management(ctx context.Context, path string, input any) (rpc.Response, error) {
	return a.Client.Exchange(ctx, http.MethodPost, path, input, "")
}

type operatorContextKey struct{}

func operatorFrom(ctx context.Context) Operator {
	user, _ := ctx.Value(operatorContextKey{}).(Operator)
	return user
}

func validOperator(user Operator) bool {
	if !operatorID.MatchString(user.ID) || user.Username == "" || len(user.Username) > 60 {
		return false
	}
	switch user.Role {
	case "Viewer", "Operator", "Engineer", "Incident Commander", "Administrator":
		return true
	default:
		return false
	}
}

func safeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

func (s *Server) authRoutes(mux *http.ServeMux) {
	s.userRoutes(mux)
	mux.HandleFunc("GET /api/v1/auth/status", func(w http.ResponseWriter, _ *http.Request) {
		s.write(w, 200, map[string]bool{"required": s.RequireAuth})
	})
	mux.HandleFunc("POST /api/v1/auth/login", s.login)
	mux.HandleFunc("GET /api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		if !s.RequireAuth {
			s.write(w, 503, map[string]string{"error": "operator sessions are not enabled"})
			return
		}
		s.write(w, 200, operatorFrom(r.Context()))
	})
	mux.HandleFunc("POST /api/v1/auth/logout", s.logout)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.RequireAuth || s.Auth == nil {
		s.write(w, 503, map[string]string{"error": "operator sessions are not enabled"})
		return
	}
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		s.write(w, 415, map[string]string{"error": "use application/json"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		s.write(w, 400, map[string]string{"error": "invalid login request"})
		return
	}
	session, err := s.Auth.Login(r.Context(), input.Username, input.Password)
	if errors.Is(err, errUnauthenticated) {
		s.write(w, 401, map[string]string{"error": "invalid credentials"})
		return
	}
	if err != nil {
		s.write(w, 503, map[string]string{"error": "authentication unavailable"})
		return
	}
	maxAge := int(time.Until(session.ExpiresAt).Seconds())
	if maxAge < 1 || maxAge > 24*60*60 {
		s.write(w, 503, map[string]string{"error": "invalid session lifetime"})
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: session.Token, Path: "/", HttpOnly: true, Secure: s.CookieSecure, SameSite: http.SameSiteStrictMode, MaxAge: maxAge, Expires: session.ExpiresAt})
	s.write(w, 200, session.User)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if !s.RequireAuth || s.Auth == nil {
		s.write(w, 503, map[string]string{"error": "operator sessions are not enabled"})
		return
	}
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		s.write(w, 401, map[string]string{"error": "authentication required"})
		return
	}
	if err = s.Auth.Logout(r.Context(), cookie.Value); err != nil {
		s.write(w, 503, map[string]string{"error": "authentication unavailable"})
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", HttpOnly: true, Secure: s.CookieSecure, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	s.write(w, 200, map[string]string{"status": "signed out"})
}

func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	if s.Auth == nil {
		s.write(w, 503, map[string]string{"error": "authentication unavailable"})
		return nil, false
	}
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		s.write(w, 401, map[string]string{"error": "authentication required"})
		return nil, false
	}
	user, err := s.Auth.Validate(r.Context(), cookie.Value)
	if errors.Is(err, errUnauthenticated) {
		s.write(w, 401, map[string]string{"error": "session expired or revoked"})
		return nil, false
	}
	if err != nil || !validOperator(user) {
		s.write(w, 503, map[string]string{"error": "authentication unavailable"})
		return nil, false
	}
	if !allowedMutation(user.Role, r.Method, r.URL.Path) {
		s.write(w, 403, map[string]string{"error": "role cannot perform this action"})
		return nil, false
	}
	return r.WithContext(context.WithValue(r.Context(), operatorContextKey{}, user)), true
}

// Unknown mutations are denied until they have an explicit role policy.
func allowedMutation(role, method, path string) bool {
	if safeMethod(method) || path == "/api/v1/auth/logout" {
		return true
	}
	if strings.HasPrefix(path, "/api/v1/incidents/saved-views") &&
		(method == http.MethodPost || method == http.MethodPut || method == http.MethodDelete) {
		return true // Personal view preferences are available to every authenticated role.
	}
	if role == "Viewer" {
		return false
	}
	if role == "Administrator" && ((method == http.MethodPost && path == "/api/v1/auth/users") ||
		(method == http.MethodPatch && strings.HasPrefix(path, "/api/v1/auth/users/"))) {
		return true
	}
	if method == http.MethodPost && path == "/api/v1/transactions" {
		return true
	}
	if method == http.MethodPost && strings.HasPrefix(path, "/api/v1/simulations") {
		return role == "Engineer" || role == "Administrator"
	}
	if method == http.MethodPost && strings.HasPrefix(path, "/api/v1/notifications/dead-letters/") && strings.HasSuffix(path, "/replay") {
		return role == "Engineer" || role == "Administrator"
	}
	if method == http.MethodPost && path == "/api/v1/incidents" {
		return role == "Operator" || role == "Incident Commander" || role == "Administrator"
	}
	if method == http.MethodPost && strings.HasPrefix(path, "/api/v1/incidents/") && strings.HasSuffix(path, "/escalations") {
		return role == "Incident Commander" || role == "Administrator"
	}
	if method == http.MethodPut && strings.HasPrefix(path, "/api/v1/incidents/") {
		return role == "Operator" || role == "Incident Commander" || role == "Administrator"
	}
	return false
}
