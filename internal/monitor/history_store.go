package monitor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
)

func (s Store) History(ctx context.Context, d session.Digest, wid, pid, mid string, q HistoryQuery) (HistoryPage, error) {
	page := HistoryPage{Data: make([]HistoryItem, 0)}
	if q.Limit < 1 || q.Limit > 50 || !historyState(q.State) {
		return page, user.ErrInternal
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var beforeAt, beforeID, upperAt, upperID any
	if c := q.cursor; c != nil {
		beforeAt, beforeID, upperAt, upperID = c.BeforeAt, c.BeforeID, c.UpperAt, c.UpperID
	}
	var decision string
	var raw []byte
	err := s.Pool.QueryRow(ctx, `SELECT decision,data FROM scheduler_private.monitor_history($1,$2::uuid,$3::uuid,$4::uuid,$5::integer,$6::text,$7::timestamptz,$8::uuid,$9::timestamptz,$10::uuid)`, d[:], selector(wid), selector(pid), selector(mid), q.Limit, q.State, beforeAt, beforeID, upperAt, upperID).Scan(&decision, &raw)
	if err != nil {
		return page, classify(ctx, err)
	}
	switch decision {
	case "unauthenticated":
		return page, session.ErrInvalid
	case "not_found":
		return page, ErrNotFound
	case "ok":
	default:
		return page, user.ErrInternal
	}
	var reply struct {
		Version string    `json:"configuration_version"`
		UpperAt time.Time `json:"upper_at"`
		UpperID string    `json:"upper_id"`
		Rows    []struct {
			Item HistoryItem `json:"item"`
			At   time.Time   `json:"position_at"`
			ID   string      `json:"position_id"`
		} `json:"rows"`
	}
	if json.Unmarshal(raw, &reply) != nil || reply.Version == "" || len(reply.Rows) > q.Limit+1 {
		return page, user.ErrInternal
	}
	page.ConfigurationVersion = reply.Version
	n := len(reply.Rows)
	if n > q.Limit {
		n = q.Limit
		last := reply.Rows[n-1]
		c := historyCursor{Version: 1, WorkspaceID: wid, ProjectID: pid, MonitorID: mid, State: q.State, BeforeAt: last.At.UTC(), BeforeID: last.ID, UpperAt: reply.UpperAt.UTC(), UpperID: reply.UpperID}
		b, _ := json.Marshal(c)
		next := base64.RawURLEncoding.EncodeToString(b)
		page.Pagination.NextAfter = &next
	}
	for _, row := range reply.Rows[:n] {
		v := row.Item
		v.CheckedAt = v.CheckedAt.UTC()
		v.FinishedAt = utcPointer(v.FinishedAt)
		page.Data = append(page.Data, v)
	}
	return page, nil
}
