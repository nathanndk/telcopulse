package incident

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var traceIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

type TraceNode struct {
	Name       string  `json:"name"`
	Kind       string  `json:"kind"`
	DurationMS float64 `json:"max_duration_ms"`
	Error      bool    `json:"error"`
}

type TraceEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type TraceRecord struct {
	ID        string      `json:"trace_id"`
	StartedAt time.Time   `json:"started_at"`
	Nodes     []TraceNode `json:"nodes"`
	Edges     []TraceEdge `json:"edges"`
}

type TraceEvidence struct {
	Configured bool          `json:"configured"`
	Source     string        `json:"source"`
	Start      time.Time     `json:"window_start"`
	End        time.Time     `json:"window_end"`
	Items      []TraceRecord `json:"items"`
}

type TraceSearcher interface {
	SearchTraces(context.Context, Incident, time.Time, time.Time) ([]TraceRecord, error)
}

type JaegerSearch struct {
	base   string
	client *http.Client
}

func NewJaegerSearch(base string, local bool) (*JaegerSearch, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("invalid Jaeger query URL")
	}
	loopback := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1" || u.Hostname() == "jaeger"
	if u.Scheme != "https" && (!local || !loopback || u.Scheme != "http") {
		return nil, errors.New("Jaeger query requires HTTPS")
	}
	return &JaegerSearch{base: strings.TrimRight(base, "/"), client: &http.Client{
		Timeout:       8 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect disabled") },
	}}, nil
}

func JaegerSearchFromEnv() (TraceSearcher, error) {
	if base := os.Getenv("JAEGER_QUERY_URL"); base != "" {
		return NewJaegerSearch(base, os.Getenv("APP_MODE") == "local")
	}
	return nil, nil
}

func (s *JaegerSearch) read(ctx context.Context, path string, limit int64, value any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+path, nil)
	if err != nil {
		return errors.New("invalid trace query")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return errors.New("trace source unavailable")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return errors.New("trace source rejected query")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || int64(len(data)) > limit {
		return errors.New("trace response exceeds limit")
	}
	if json.Unmarshal(data, value) != nil {
		return errors.New("invalid trace response")
	}
	return nil
}

func (s *JaegerSearch) SearchTraces(ctx context.Context, incident Incident, start, end time.Time) ([]TraceRecord, error) {
	if !splunkIdentifier.MatchString(incident.Service) || (incident.Environment != "development" && incident.Environment != "staging") || !end.After(start) || end.Sub(start) > 73*time.Hour {
		return nil, invalid(errors.New("invalid trace search scope"))
	}
	if isPurchasePath(incident.Service) && !incident.DetectedAt.IsZero() && !incident.DetectedAt.Before(start) && !incident.DetectedAt.After(end) {
		focusedEnd := incident.DetectedAt.Add(5 * time.Minute)
		if focusedEnd.After(end) {
			focusedEnd = end
		}
		items, err := s.searchTraces(ctx, incident, start, focusedEnd, "api-gateway", "POST /api/v1/transactions")
		if err != nil || len(items) > 0 {
			return items, err
		}
	}
	return s.searchTraces(ctx, incident, start, end, incident.Service, "")
}

func (s *JaegerSearch) searchTraces(ctx context.Context, incident Incident, start, end time.Time, service, operation string) ([]TraceRecord, error) {
	q := url.Values{
		"query.serviceName":  {service},
		"query.startTimeMin": {start.UTC().Format(time.RFC3339Nano)},
		"query.startTimeMax": {end.UTC().Format(time.RFC3339Nano)},
		"query.searchDepth":  {"8"},
	}
	if operation != "" {
		q.Set("query.operationName", operation)
	}
	var found struct {
		Summaries []struct {
			ID string `json:"traceId"`
		} `json:"summaries"`
	}
	if err := s.read(ctx, "/api/v3/trace-summaries?"+q.Encode(), 128<<10, &found); err != nil {
		return nil, err
	}
	if found.Summaries == nil || len(found.Summaries) > 8 {
		return nil, errors.New("invalid trace search response")
	}
	items := make([]TraceRecord, 0, 4)
	for _, summary := range found.Summaries {
		if len(items) == 4 {
			break
		}
		if !traceIDPattern.MatchString(summary.ID) {
			return nil, errors.New("invalid trace identifier")
		}
		var payload jaegerTraceEnvelope
		if err := s.read(ctx, "/api/v3/traces/"+summary.ID, 512<<10, &payload); err != nil {
			return nil, err
		}
		if payload.Result.Resources == nil {
			return nil, errors.New("invalid trace response")
		}
		record, ok := projectTrace(payload, summary.ID, incident, start, end)
		if ok {
			items = append(items, record)
		}
	}
	return items, nil
}

type jaegerAttribute struct {
	Key   string `json:"key"`
	Value struct {
		String string `json:"stringValue"`
		Int    string `json:"intValue"`
	} `json:"value"`
}

type jaegerSpan struct {
	ID       string            `json:"spanId"`
	ParentID string            `json:"parentSpanId"`
	TraceID  string            `json:"traceId"`
	Start    string            `json:"startTimeUnixNano"`
	End      string            `json:"endTimeUnixNano"`
	Attrs    []jaegerAttribute `json:"attributes"`
	Status   struct {
		Code json.RawMessage `json:"code"`
	} `json:"status"`
}

type jaegerTraceEnvelope struct {
	Result struct {
		Resources []struct {
			Resource struct {
				Attrs []jaegerAttribute `json:"attributes"`
			} `json:"resource"`
			Scopes []struct {
				Spans []jaegerSpan `json:"spans"`
			} `json:"scopeSpans"`
		} `json:"resourceSpans"`
	} `json:"result"`
}

func attrValue(attrs []jaegerAttribute, key string) string {
	for _, attr := range attrs {
		if attr.Key == key {
			if attr.Value.String != "" {
				return attr.Value.String
			}
			return attr.Value.Int
		}
	}
	return ""
}

func projectTrace(payload jaegerTraceEnvelope, id string, incident Incident, start, end time.Time) (TraceRecord, bool) {
	record := TraceRecord{ID: id, Nodes: []TraceNode{}, Edges: []TraceEdge{}}
	nodes := map[string]*TraceNode{}
	owners := map[string]string{}
	edges := map[TraceEdge]bool{}
	environmentMatched, environmentConflict, serviceMatched := false, false, false
	var spans []struct {
		owner string
		span  jaegerSpan
	}
	for _, resource := range payload.Result.Resources {
		service := attrValue(resource.Resource.Attrs, "service.name")
		if !splunkIdentifier.MatchString(service) {
			continue
		}
		for _, scope := range resource.Scopes {
			for _, span := range scope.Spans {
				if span.TraceID != id || len(span.ID) != 16 {
					continue
				}
				if environment := attrValue(span.Attrs, "deployment.environment.name"); environment == incident.Environment {
					environmentMatched = true
				} else if environment != "" {
					environmentConflict = true
				}
				if service == incident.Service {
					serviceMatched = true
				}
				spans = append(spans, struct {
					owner string
					span  jaegerSpan
				}{service, span})
				owners[span.ID] = service
			}
		}
	}
	if !environmentMatched || environmentConflict || !serviceMatched || len(spans) > 1000 {
		return TraceRecord{}, false
	}
	for _, item := range spans {
		span, service := item.span, item.owner
		startNano, err1 := strconv.ParseInt(span.Start, 10, 64)
		endNano, err2 := strconv.ParseInt(span.End, 10, 64)
		if err1 != nil || err2 != nil || endNano < startNano {
			continue
		}
		at := time.Unix(0, startNano).UTC()
		if record.StartedAt.IsZero() || at.Before(record.StartedAt) {
			record.StartedAt = at
		}
		db := attrValue(span.Attrs, "db.system.name")
		name, kind := service, "service"
		if db != "" && splunkIdentifier.MatchString(db) {
			name, kind = db, "database"
			edges[TraceEdge{From: service, To: name}] = true
		} else if parent := owners[span.ParentID]; parent != "" && parent != service {
			edges[TraceEdge{From: parent, To: service}] = true
		}
		key := kind + ":" + name
		node := nodes[key]
		if node == nil {
			node = &TraceNode{Name: name, Kind: kind}
			nodes[key] = node
		}
		ms := float64(endNano-startNano) / 1e6
		if ms > node.DurationMS {
			node.DurationMS = ms
		}
		status := string(span.Status.Code)
		code, _ := strconv.Atoi(attrValue(span.Attrs, "http.response.status_code"))
		if status == `"STATUS_CODE_ERROR"` || status == "2" || code >= 500 || attrValue(span.Attrs, "transaction.outcome") == "FAILED" {
			node.Error = true
		}
	}
	if record.StartedAt.IsZero() || record.StartedAt.After(end) || record.StartedAt.Before(start.Add(-time.Hour)) {
		return TraceRecord{}, false
	}
	for _, node := range nodes {
		record.Nodes = append(record.Nodes, *node)
	}
	for edge := range edges {
		record.Edges = append(record.Edges, edge)
	}
	sort.Slice(record.Nodes, func(a, b int) bool { return record.Nodes[a].Name < record.Nodes[b].Name })
	sort.Slice(record.Edges, func(a, b int) bool {
		if record.Edges[a].From == record.Edges[b].From {
			return record.Edges[a].To < record.Edges[b].To
		}
		return record.Edges[a].From < record.Edges[b].From
	})
	return record, true
}
