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
	"slices"
	"strconv"
	"strings"
	"time"
)

var splunkIdentifier = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,99}$`)
var subscriberNumber = regexp.MustCompile(`(?:\+?62|0)8[0-9]{8,11}`)

type LogEvent struct {
	At            string `json:"at"`
	Level         string `json:"level"`
	Service       string `json:"service"`
	Message       string `json:"message"`
	TraceID       string `json:"trace_id"`
	TransactionID string `json:"transaction_id"`
	ErrorCode     string `json:"error_code"`
}

type LogEvidence struct {
	Configured bool       `json:"configured"`
	Source     string     `json:"source"`
	Start      time.Time  `json:"window_start"`
	End        time.Time  `json:"window_end"`
	Items      []LogEvent `json:"items"`
}

type LogSearcher interface {
	Search(context.Context, Incident, time.Time, time.Time) ([]LogEvent, error)
}

type SplunkSearch struct {
	endpoint string
	token    string
	index    string
	client   *http.Client
}

// NewSplunkSearch accepts only a fixed Search API path. Browser requests never
// choose the endpoint, index or SPL. TLS is required outside loopback tests.
func NewSplunkSearch(endpoint, token, index string, local bool) (*SplunkSearch, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/services/search/v2/jobs" {
		return nil, errors.New("invalid Splunk Search endpoint")
	}
	loopback := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if u.Scheme != "https" && (!local || !loopback || u.Scheme != "http") {
		return nil, errors.New("Splunk Search requires HTTPS")
	}
	if len(token) < 16 || len(token) > 4096 || strings.ContainsAny(token, "\r\n") || !splunkIdentifier.MatchString(index) {
		return nil, errors.New("invalid Splunk Search configuration")
	}
	return &SplunkSearch{endpoint: endpoint, token: token, index: index, client: &http.Client{
		Timeout:       8 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect disabled") },
	}}, nil
}

// SplunkSearchFromEnv leaves local demo mode unconfigured unless a private
// search token file and matching endpoint are both supplied.
func SplunkSearchFromEnv() (LogSearcher, error) {
	endpoint, path := os.Getenv("SPLUNK_SEARCH_URL"), os.Getenv("SPLUNK_SEARCH_TOKEN_FILE")
	if endpoint == "" && path == "" {
		return nil, nil
	}
	if endpoint == "" || path == "" {
		return nil, errors.New("Splunk Search URL and token file must be configured together")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot read Splunk Search token file")
	}
	data, readErr := io.ReadAll(io.LimitReader(file, 4097))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(data) > 4096 {
		return nil, errors.New("invalid Splunk Search token file")
	}
	index := os.Getenv("SPLUNK_SEARCH_INDEX")
	if index == "" {
		index = "telcopulse"
	}
	return NewSplunkSearch(endpoint, strings.TrimSpace(string(data)), index, os.Getenv("APP_MODE") == "local")
}

func logWindow(i Incident, choice string, now time.Time) (time.Time, time.Time, error) {
	hours := 24
	switch choice {
	case "", "24h":
	case "2h":
		hours = 2
	case "72h":
		hours = 72
	default:
		return time.Time{}, time.Time{}, invalid(errors.New("invalid log time window"))
	}
	start := i.DetectedAt.UTC().Add(-15 * time.Minute)
	end := i.DetectedAt.UTC().Add(time.Duration(hours) * time.Hour)
	if end.After(now) {
		end = now
	}
	if !end.After(start) {
		return time.Time{}, time.Time{}, invalid(errors.New("invalid incident detection time"))
	}
	return start, end, nil
}

func (s *SplunkSearch) Search(ctx context.Context, incident Incident, start, end time.Time) ([]LogEvent, error) {
	if !splunkIdentifier.MatchString(incident.Service) || !slices.Contains([]string{"development", "staging"}, incident.Environment) || !end.After(start) || end.Sub(start) > 73*time.Hour {
		return nil, invalid(errors.New("invalid log search scope"))
	}
	values := url.Values{
		"search":        {"search index=" + s.index + " sourcetype=\"_json\" environment=\"" + incident.Environment + "\" service=\"" + incident.Service + "\" | head 50 | table _time level environment service msg trace_id transaction_id error_code"},
		"exec_mode":     {"oneshot"},
		"output_mode":   {"json"},
		"earliest_time": {strconv.FormatInt(start.Unix(), 10)},
		"latest_time":   {strconv.FormatInt(end.Unix(), 10)},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, errors.New("invalid log search request")
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, errors.New("Splunk Search unavailable")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("Splunk Search rejected request")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (256<<10)+1))
	if err != nil || len(data) > 256<<10 {
		return nil, errors.New("Splunk Search response exceeds limit")
	}
	var payload struct {
		Results []struct {
			At            string `json:"_time"`
			Level         string `json:"level"`
			Environment   string `json:"environment"`
			Service       string `json:"service"`
			Message       string `json:"msg"`
			TraceID       string `json:"trace_id"`
			TransactionID string `json:"transaction_id"`
			ErrorCode     string `json:"error_code"`
		} `json:"results"`
	}
	if json.Unmarshal(data, &payload) != nil || payload.Results == nil || len(payload.Results) > 50 {
		return nil, errors.New("invalid Splunk Search response")
	}
	items := make([]LogEvent, 0, len(payload.Results))
	for _, row := range payload.Results {
		if row.Environment != incident.Environment || row.Service != incident.Service || !slices.Contains([]string{"INFO", "WARN", "ERROR"}, row.Level) {
			continue
		}
		items = append(items, LogEvent{At: clipRunes(row.At, 80), Level: row.Level, Service: row.Service,
			Message: maskSubscriberText(clipRunes(row.Message, 500)), TraceID: clipRunes(row.TraceID, 64),
			TransactionID: clipRunes(row.TransactionID, 100), ErrorCode: clipRunes(row.ErrorCode, 100)})
	}
	return items, nil
}

func clipRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes)
}

func maskSubscriberText(value string) string {
	return subscriberNumber.ReplaceAllStringFunc(value, func(number string) string {
		prefix := 5
		if strings.HasPrefix(number, "08") {
			prefix = 4
		}
		if strings.HasPrefix(number, "+") {
			prefix = 6
		}
		return number[:prefix] + "*****" + number[len(number)-3:]
	})
}
