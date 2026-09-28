// Package logexport provides optional transports for vendor-neutral structured logs.
package logexport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HEC is a bounded Splunk event transport. Acceptance is not index acknowledgment.
type HEC struct {
	endpoint, token, index string
	client                 *http.Client
}

// NewHEC requires TLS except explicitly enabled loopback development receivers.
func NewHEC(endpoint, token, index string, local bool) (*HEC, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/services/collector/event" {
		return nil, errors.New("invalid HEC endpoint")
	}
	loopback := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if u.Scheme != "https" && (!local || !loopback || u.Scheme != "http") {
		return nil, errors.New("HEC requires HTTPS")
	}
	if len(token) < 1 || len(token) > 4096 || strings.ContainsAny(token, "\r\n") || len(index) > 100 {
		return nil, errors.New("invalid HEC configuration")
	}
	return &HEC{endpoint: endpoint, token: token, index: index, client: &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect disabled") }}}, nil
}

// Send submits one structured event. Retries and durable buffering belong to the caller.
func (h *HEC) Send(ctx context.Context, event []byte) error {
	if len(event) == 0 || len(event) > 65536 {
		return errors.New("log event exceeds bounds")
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(event, &object) != nil || object == nil {
		return errors.New("log event must be a JSON object")
	}
	payload, err := json.Marshal(struct {
		Event      json.RawMessage `json:"event"`
		Index      string          `json:"index,omitempty"`
		SourceType string          `json:"sourcetype"`
	}{event, h.index, "_json"})
	if err != nil {
		return errors.New("invalid log event")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, h.endpoint, bytes.NewReader(payload))
	if err != nil {
		return errors.New("invalid HEC request")
	}
	request.Header.Set("Authorization", "Splunk "+h.token)
	request.Header.Set("Content-Type", "application/json")
	response, err := h.client.Do(request)
	if err != nil {
		return errors.New("HEC transport failed")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return errors.New("HEC rejected request")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil || len(body) > 4096 {
		return errors.New("invalid HEC response")
	}
	var result struct {
		Code *int `json:"code"`
	}
	if json.Unmarshal(body, &result) != nil || result.Code == nil || *result.Code != 0 {
		return errors.New("HEC did not accept event")
	}
	return nil
}
