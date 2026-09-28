package notification

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	rt "telcopulse/services/shared/runtime"
)

type deadLetterRecord struct {
	SourceTopic     string     `json:"source_topic"`
	SourcePartition int32      `json:"source_partition"`
	SourceOffset    int64      `json:"source_offset"`
	Reason          string     `json:"reason"`
	CreatedAt       time.Time  `json:"created_at"`
	PayloadBytes    int        `json:"payload_bytes"`
	PayloadSHA256   string     `json:"payload_sha256"`
	Publication     string     `json:"publication"`
	ReplayStatus    *string    `json:"replay_status,omitempty"`
	ReplayActor     *string    `json:"replay_actor,omitempty"`
	ReplayedAt      *time.Time `json:"replayed_at,omitempty"`
}

type deadLetterPage struct {
	Items []deadLetterRecord `json:"items"`
	More  bool               `json:"more"`
	Next  string             `json:"next,omitempty"`
}

type deadLetterCursor struct {
	At        time.Time `json:"at"`
	Topic     string    `json:"topic"`
	Partition int32     `json:"partition"`
	Offset    int64     `json:"offset"`
	Reason    string    `json:"reason"`
}

func validDeadLetterReason(reason string) bool {
	return reason == "" || reason == "invalid purchase event" || reason == "event identity conflict"
}

func listDeadLetters(pool *pgxpool.Pool, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		for key := range r.URL.Query() {
			if key != "limit" && key != "cursor" && key != "reason" {
				rt.JSON(w, 400, map[string]string{"error": "unknown dead-letter filter"})
				return
			}
		}
		reason := r.URL.Query().Get("reason")
		if !validDeadLetterReason(reason) {
			rt.JSON(w, 422, map[string]string{"error": "invalid dead-letter reason"})
			return
		}
		limit := 25
		if raw := r.URL.Query().Get("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 || parsed > 50 {
				rt.JSON(w, 422, map[string]string{"error": "invalid dead-letter page size"})
				return
			}
			limit = parsed
		}
		var at any
		var topic any
		var partition any
		var offset any
		if raw := r.URL.Query().Get("cursor"); raw != "" {
			if len(raw) > 1024 {
				rt.JSON(w, 422, map[string]string{"error": "invalid dead-letter cursor"})
				return
			}
			data, err := base64.RawURLEncoding.DecodeString(raw)
			var cursor deadLetterCursor
			if err == nil {
				decoder := json.NewDecoder(bytes.NewReader(data))
				decoder.DisallowUnknownFields()
				err = decoder.Decode(&cursor)
				if err == nil && decoder.Decode(&struct{}{}) != io.EOF {
					err = errors.New("trailing cursor data")
				}
			}
			if err != nil || cursor.At.IsZero() || cursor.Topic == "" || cursor.Partition < 0 || cursor.Offset < 0 || cursor.Reason != reason {
				rt.JSON(w, 422, map[string]string{"error": "invalid dead-letter cursor"})
				return
			}
			at, topic, partition, offset = cursor.At, cursor.Topic, cursor.Partition, cursor.Offset
		}
		rows, err := pool.Query(r.Context(), `
SELECT d.topic,d.partition_id,d.message_offset,d.reason,d.created_at,
       octet_length(d.payload),encode(sha256(d.payload),'hex'),
       CASE WHEN e.event_id IS NULL THEN 'UNTRACKED'
            WHEN e.published_at IS NULL THEN 'PENDING' ELSE 'PUBLISHED' END,
       replay.status,replay.actor,replay.attempted_at
FROM notification.dead_letters d
LEFT JOIN notification.event_outbox e
 ON e.event_id='notification.dead-letter:' || d.topic || ':' || d.partition_id || ':' || d.message_offset
LEFT JOIN LATERAL (
 SELECT status,actor,attempted_at FROM notification.dead_letter_replays
 WHERE topic=d.topic AND partition_id=d.partition_id AND message_offset=d.message_offset
 ORDER BY attempted_at DESC,idempotency_key DESC LIMIT 1
) replay ON TRUE
WHERE ($1::timestamptz IS NULL OR
       (d.created_at,d.topic,d.partition_id,d.message_offset)<($1::timestamptz,$2::text,$3::integer,$4::bigint))
  AND ($5::text='' OR d.reason=$5)
ORDER BY d.created_at DESC,d.topic DESC,d.partition_id DESC,d.message_offset DESC
LIMIT $6`, at, topic, partition, offset, reason, limit+1)
		if err != nil {
			log.Error("list dead letters", "error", err)
			rt.JSON(w, 503, map[string]string{"error": "dead-letter register unavailable"})
			return
		}
		defer rows.Close()
		page := deadLetterPage{Items: []deadLetterRecord{}}
		for rows.Next() {
			var item deadLetterRecord
			if err = rows.Scan(&item.SourceTopic, &item.SourcePartition, &item.SourceOffset, &item.Reason, &item.CreatedAt, &item.PayloadBytes, &item.PayloadSHA256, &item.Publication, &item.ReplayStatus, &item.ReplayActor, &item.ReplayedAt); err != nil {
				break
			}
			page.Items = append(page.Items, item)
		}
		if err == nil {
			err = rows.Err()
		}
		if err != nil {
			log.Error("scan dead letters", "error", err)
			rt.JSON(w, 503, map[string]string{"error": "dead-letter register unavailable"})
			return
		}
		if len(page.Items) > limit {
			page.More = true
			page.Items = page.Items[:limit]
			last := page.Items[len(page.Items)-1]
			encoded, encodeErr := json.Marshal(deadLetterCursor{At: last.CreatedAt, Topic: last.SourceTopic, Partition: last.SourcePartition, Offset: last.SourceOffset, Reason: reason})
			if encodeErr != nil {
				log.Error("encode dead-letter cursor", "error", encodeErr)
				rt.JSON(w, 503, map[string]string{"error": "dead-letter register unavailable"})
				return
			}
			page.Next = base64.RawURLEncoding.EncodeToString(encoded)
		}
		rt.JSON(w, 200, page)
	}
}
