package monitor

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strconv"
	"time"
)

type HistoryItem struct {
	ID                   string     `json:"id"`
	ConfigurationVersion string     `json:"configuration_version"`
	CheckedAt            time.Time  `json:"checked_at"`
	FinishedAt           *time.Time `json:"finished_at"`
	Status               string     `json:"status"`
	HTTPStatus           *int       `json:"http_status"`
	DurationMS           *int       `json:"duration_ms"`
	FailureCode          *string    `json:"failure_code"`
}
type HistoryPage struct {
	ConfigurationVersion string        `json:"configuration_version"`
	Data                 []HistoryItem `json:"data"`
	Pagination           struct {
		NextAfter *string `json:"next_after"`
	} `json:"pagination"`
}

// Cursor contents are a pagination position, never an authorization capability.
// Immutable committed claim time + UUID survive completion and cursor-row cleanup.
type historyCursor struct {
	Version     int       `json:"v"`
	WorkspaceID string    `json:"w"`
	ProjectID   string    `json:"p"`
	MonitorID   string    `json:"m"`
	State       string    `json:"s"`
	BeforeAt    time.Time `json:"b"`
	BeforeID    string    `json:"i"`
	UpperAt     time.Time `json:"u"`
	UpperID     string    `json:"j"`
}
type HistoryQuery struct {
	Limit  int
	State  string
	cursor *historyCursor
}

func historyState(s string) bool {
	switch s {
	case "all", "success", "failure", "unknown", "running", "canceled", "expired":
		return true
	}
	return false
}
func historySelectors(raw, wid, pid, mid string) (HistoryQuery, bool) {
	q := HistoryQuery{Limit: 20, State: "all"}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return q, false
	}
	var after string
	for k, v := range values {
		if len(v) != 1 {
			return q, false
		}
		switch k {
		case "limit":
			n, e := strconv.Atoi(v[0])
			if e != nil || n < 1 || n > 50 || strconv.Itoa(n) != v[0] {
				return q, false
			}
			q.Limit = n
		case "state":
			if !historyState(v[0]) {
				return q, false
			}
			q.State = v[0]
		case "after":
			after = v[0]
			if after == "" || len(after) > 1024 {
				return q, false
			}
		default:
			return q, false
		}
	}
	if after != "" {
		b, e := base64.RawURLEncoding.DecodeString(after)
		if e != nil {
			return q, false
		}
		var c historyCursor
		if json.Unmarshal(b, &c) != nil {
			return q, false
		}
		canonical, _ := json.Marshal(c)
		if base64.RawURLEncoding.EncodeToString(canonical) != after || c.Version != 1 || c.WorkspaceID != wid || c.ProjectID != pid || c.MonitorID != mid || c.State != q.State || !validUUID(c.BeforeID) || !validUUID(c.UpperID) || c.BeforeAt.IsZero() || c.UpperAt.IsZero() || c.BeforeAt.Year() < 1 || c.UpperAt.Year() < 1 || c.BeforeAt.After(c.UpperAt) || (c.BeforeAt.Equal(c.UpperAt) && c.BeforeID > c.UpperID) {
			return q, false
		}
		q.cursor = &c
	}
	return q, true
}
