package incident

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
)

// ReportFilter narrows immutable learning records without mutating or
// reinterpreting the report that was published at an incident revision.
type ReportFilter struct {
	Environment string
	Service     string
	Severity    string
	Search      string
	Limit       int
	Cursor      string
}

type ReportSummary struct {
	IncidentID          string    `json:"incident_id"`
	IncidentTitle       string    `json:"incident_title"`
	Environment         string    `json:"environment"`
	Service             string    `json:"service"`
	Severity            string    `json:"severity"`
	IncidentVersion     int64     `json:"incident_version"`
	GeneratedAt         time.Time `json:"generated_at"`
	GeneratedBy         string    `json:"generated_by"`
	Summary             string    `json:"summary"`
	RootCause           string    `json:"root_cause"`
	ContributingFactors string    `json:"contributing_factors"`
	Mitigation          string    `json:"mitigation"`
	Resolution          string    `json:"resolution"`
	WhatWentWell        string    `json:"what_went_well"`
	WhatWentWrong       string    `json:"what_went_wrong"`
}

type ReportPage struct {
	Items []ReportSummary `json:"items"`
	More  bool            `json:"more"`
	Next  string          `json:"next,omitempty"`
}

type reportCursor struct {
	Environment string    `json:"environment"`
	Service     string    `json:"service"`
	Severity    string    `json:"severity"`
	Search      string    `json:"search"`
	At          time.Time `json:"at"`
	ID          string    `json:"id"`
}

// Postmortems pages immutable snapshots by generation time. Incident title,
// service and severity are current metadata for navigation/filtering; report
// narrative columns come only from the frozen postmortem document.
func (s Store) Postmortems(ctx context.Context, filter ReportFilter) (ReportPage, error) {
	out := ReportPage{Items: []ReportSummary{}}
	filter.Service = strings.TrimSpace(filter.Service)
	filter.Search = strings.TrimSpace(filter.Search)
	if !slices.Contains([]string{"development", "staging"}, filter.Environment) ||
		len(filter.Service) > 100 || len(filter.Search) > 100 ||
		(filter.Severity != "" && !slices.Contains([]string{"SEV-1", "SEV-2", "SEV-3", "SEV-4"}, filter.Severity)) ||
		filter.Limit < 1 || filter.Limit > 100 {
		return out, invalid(errors.New("invalid postmortem filters"))
	}
	var boundary reportCursor
	if filter.Cursor != "" {
		if len(filter.Cursor) > 1024 {
			return out, invalid(errors.New("invalid postmortem cursor"))
		}
		raw, err := base64.RawURLEncoding.DecodeString(filter.Cursor)
		if err != nil || json.Unmarshal(raw, &boundary) != nil || boundary.At.IsZero() ||
			!incidentIDPattern.MatchString(boundary.ID) || boundary.Environment != filter.Environment ||
			boundary.Service != filter.Service || boundary.Severity != filter.Severity || boundary.Search != filter.Search {
			return out, invalid(errors.New("invalid postmortem cursor"))
		}
	}
	rows, err := s.Pool.Query(ctx, `SELECT p.incident_id,r.document->>'title',r.environment,r.document->>'service',
	       r.document->>'severity',p.incident_version,p.generated_at,
	       COALESCE(p.document->>'generated_by',''),LEFT(COALESCE(p.document->>'summary',''),320),
	       LEFT(COALESCE(p.document->>'root_cause',''),400),
	       LEFT(COALESCE(p.document->>'contributing_factors',''),320),
	       LEFT(COALESCE(p.document->>'mitigation',''),320),
	       LEFT(COALESCE(p.document->>'resolution',''),320),
	       LEFT(COALESCE(p.document->>'what_went_well',''),320),
	       LEFT(COALESCE(p.document->>'what_went_wrong',''),320)
	FROM incident.postmortems p JOIN incident.records r ON r.id=p.incident_id
	WHERE r.environment=$1 AND ($2='' OR r.document->>'service'=$2)
	  AND ($3='' OR r.document->>'severity'=$3)
	  AND ($4='' OR strpos(lower(r.document->>'title'),lower($4))>0
	       OR strpos(lower(p.document->>'summary'),lower($4))>0
	       OR strpos(lower(p.document->>'root_cause'),lower($4))>0
	       OR strpos(lower(p.document->>'contributing_factors'),lower($4))>0
	       OR strpos(lower(p.document->>'mitigation'),lower($4))>0
	       OR strpos(lower(p.document->>'what_went_well'),lower($4))>0
	       OR strpos(lower(p.document->>'what_went_wrong'),lower($4))>0
	       OR strpos(lower(p.incident_id),lower($4))>0)
	  AND ($5='' OR (p.generated_at,p.incident_id)<($6,$5))
	ORDER BY p.generated_at DESC,p.incident_id DESC LIMIT $7`, filter.Environment, filter.Service,
		filter.Severity, filter.Search, boundary.ID, boundary.At, filter.Limit+1)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var item ReportSummary
		if err := rows.Scan(&item.IncidentID, &item.IncidentTitle, &item.Environment, &item.Service,
			&item.Severity, &item.IncidentVersion, &item.GeneratedAt, &item.GeneratedBy,
			&item.Summary, &item.RootCause, &item.ContributingFactors, &item.Mitigation,
			&item.Resolution, &item.WhatWentWell, &item.WhatWentWrong); err != nil {
			return out, err
		}
		out.Items = append(out.Items, item)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	if len(out.Items) > filter.Limit {
		out.Items = out.Items[:filter.Limit]
		out.More = true
		last := out.Items[len(out.Items)-1]
		boundary = reportCursor{Environment: filter.Environment, Service: filter.Service,
			Severity: filter.Severity, Search: filter.Search, At: last.GeneratedAt, ID: last.IncidentID}
		encoded, err := json.Marshal(boundary)
		if err != nil {
			return ReportPage{}, err
		}
		out.Next = base64.RawURLEncoding.EncodeToString(encoded)
	}
	return out, nil
}
