//go:build schedulerfixture && linux

package scheduler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// TargetDirectory is an offline, immutable fixture allowlist. It performs no
// lookup or dial. None of its secrets or raw fixture addresses are serializable.
type TargetDirectory struct {
	keys     map[int64][]byte
	fixtures map[string]map[netip.Addr]bool
}

func (TargetDirectory) Format(f fmt.State, _ rune) {
	_, _ = io.WriteString(f, "<fixture target directory>")
}

type FixtureTarget struct {
	URL       string
	Addresses []netip.Addr
}
type TargetDigest struct {
	Generation string `json:"generation"`
	OriginKey  string `json:"origin_key"`
	IPKey      string `json:"ip_key"`
}
type CatalogEntry struct {
	WorkspaceID          string         `json:"workspace_id"`
	ProjectID            string         `json:"project_id"`
	MonitorID            string         `json:"monitor_id"`
	ConfigurationVersion string         `json:"configuration_version"`
	Keys                 []TargetDigest `json:"keys"`
}

func canonicalOrigin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.User != nil || u.Opaque != "" || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", ErrCandidate
	}
	host := strings.ToLower(u.Hostname())
	if strings.ContainsAny(host, "%\\\x00") {
		return "", ErrCandidate
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 || (n != 80 && n != 443) {
		return "", ErrCandidate
	}
	return u.Scheme + "://" + net.JoinHostPort(host, strconv.Itoa(n)), nil
}
func NewTargetDirectory(keys map[int64][]byte, fixtures []FixtureTarget) (TargetDirectory, error) {
	d := TargetDirectory{keys: make(map[int64][]byte), fixtures: make(map[string]map[netip.Addr]bool)}
	if len(keys) < 1 || len(keys) > 2 || len(fixtures) < 1 || len(fixtures) > 1000 {
		return TargetDirectory{}, ErrCandidate
	}
	var generations []int64
	for g, key := range keys {
		if g < 1 || len(key) < 32 || len(key) > 64 {
			return TargetDirectory{}, ErrCandidate
		}
		d.keys[g] = append([]byte(nil), key...)
		generations = append(generations, g)
	}
	sort.Slice(generations, func(i, j int) bool { return generations[i] < generations[j] })
	if len(generations) == 2 && generations[1]-generations[0] != 1 {
		return TargetDirectory{}, ErrCandidate
	}
	for _, f := range fixtures {
		origin, err := canonicalOrigin(f.URL)
		if err != nil || len(f.Addresses) < 1 || len(f.Addresses) > 64 {
			return TargetDirectory{}, ErrCandidate
		}
		if d.fixtures[origin] == nil {
			d.fixtures[origin] = map[netip.Addr]bool{}
		}
		for _, a := range f.Addresses {
			if !a.IsValid() || a.Zone() != "" || a.Is4In6() {
				return TargetDirectory{}, ErrCandidate
			}
			d.fixtures[origin][a] = true
		}
	}
	return d, nil
}
func targetHash(secret []byte, kind string, data []byte) string {
	h := hmac.New(sha256.New, secret)
	_, _ = h.Write([]byte("devpilot-target-v1\x00" + kind + "\x00"))
	_, _ = h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// Bind must receive the literal IP already validated by the original executor.
// It never re-resolves or chooses a different address. This check only narrows
// fixture permission; it cannot bypass the executor's DNS/IP/TLS checks.
func (d TargetDirectory) Bind(h Hint, raw string, address netip.Addr) (CatalogEntry, error) {
	for _, id := range []string{h.WorkspaceID, h.ProjectID, h.MonitorID} {
		var u pgtype.UUID
		if u.Scan(id) != nil || !u.Valid || strings.ToLower(id) != id {
			return CatalogEntry{}, ErrCandidate
		}
	}
	v, err := strconv.ParseInt(h.ConfigurationVersion, 10, 64)
	if err != nil || v < 1 || strconv.FormatInt(v, 10) != h.ConfigurationVersion {
		return CatalogEntry{}, ErrCandidate
	}
	origin, err := canonicalOrigin(raw)
	if err != nil || !d.fixtures[origin][address] {
		return CatalogEntry{}, ErrCandidate
	}
	e := CatalogEntry{WorkspaceID: h.WorkspaceID, ProjectID: h.ProjectID, MonitorID: h.MonitorID, ConfigurationVersion: h.ConfigurationVersion}
	var gs []int64
	for g := range d.keys {
		gs = append(gs, g)
	}
	sort.Slice(gs, func(i, j int) bool { return gs[i] < gs[j] })
	for _, g := range gs {
		native := address.AsSlice()
		family := byte(6)
		if address.Is4() {
			family = 4
		}
		ip := append([]byte{family}, native...)
		e.Keys = append(e.Keys, TargetDigest{Generation: strconv.FormatInt(g, 10), OriginKey: targetHash(d.keys[g], "origin", []byte(origin)), IPKey: targetHash(d.keys[g], "ip", ip)})
	}
	return e, nil
}

// TargetPermit confirms only the DB reservation. It is never proof that a
// Supervisor installed network rules, nor permission for a caller to dial.
type TargetPermit struct {
	PermitID       string    `json:"permit_id"`
	PermitDeadline time.Time `json:"permit_deadline"`
	CommittedAt    time.Time `json:"committed_at"`
}

func (s Store) ReserveTarget(ctx context.Context, h Hint, lease Lease, digest TargetDigest) (TargetPermit, error) {
	if !lease.matches(h) {
		return TargetPermit{}, ErrCandidate
	}
	ctx, cancel := context.WithTimeout(ctx, transactionBudget)
	defer cancel()
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return TargetPermit{}, ErrUnavailable
	}
	defer rollback(tx)
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT scheduler_private.reserve_target($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,$7::uuid,$8::bigint,$9::bigint,$10::text,$11::text)`, s.Incarnation, h.WorkspaceID, h.ProjectID, h.MonitorID, h.OwnerUserID, h.MembershipID, h.RunID, lease.FencingToken, digest.Generation, digest.OriginKey, digest.IPKey).Scan(&raw)
	if err != nil {
		return TargetPermit{}, databaseError(err)
	}
	var p TargetPermit
	if json.Unmarshal(raw, &p) != nil || p.PermitID == "" || p.CommittedAt.IsZero() || !p.PermitDeadline.After(p.CommittedAt) {
		return TargetPermit{}, ErrUnavailable
	}
	if tx.Commit(ctx) != nil {
		return TargetPermit{}, ErrCommitUnknown
	}
	return p, nil
}
func (s Store) RotateTargetBudgetKeys(ctx context.Context, generation int64, catalog []CatalogEntry) error {
	if generation < 1 || len(catalog) < 1 || len(catalog) > 1000 {
		return ErrCandidate
	}
	raw, err := json.Marshal(catalog)
	if err != nil || len(raw) > 524288 {
		return ErrCandidate
	}
	ctx, cancel := context.WithTimeout(ctx, transactionBudget)
	defer cancel()
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return ErrUnavailable
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT scheduler_private.rotate_target_budget_keys($1::uuid,$2::bigint,$3::jsonb)`, s.Incarnation, generation, raw); err != nil {
		return databaseError(err)
	}
	if tx.Commit(ctx) != nil {
		return ErrCommitUnknown
	}
	return nil
}
