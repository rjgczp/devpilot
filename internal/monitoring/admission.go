package monitoring

import (
	"sync"
	"time"
)

const maxGateRecords = 1024

type bucket struct {
	tokens      float64
	at          time.Time
	initialized bool
}

func (b bucket) refill(now time.Time, rate float64) bucket {
	if !b.initialized {
		return bucket{tokens: rate, at: now, initialized: true}
	}
	if now.After(b.at) {
		b.tokens = min(rate, b.tokens+now.Sub(b.at).Seconds()*rate)
		b.at = now
	}
	return b
}

type gateRecord struct {
	active                              int
	bucket                              bucket
	lastStart, nextAllowed, retainUntil time.Time
}
type admissionGate struct {
	mu      sync.Mutex
	active  int
	global  bucket
	records map[string]gateRecord
}

func (g *admissionGate) acquire(s Snapshot, now time.Time) (func(), bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.records == nil {
		g.records = make(map[string]gateRecord)
	}
	for k, r := range g.records {
		if r.active == 0 && !now.Before(r.retainUntil) {
			delete(g.records, k)
		}
	}
	keys := [3]string{"w:" + s.scope.WorkspaceID, "p:" + s.scope.WorkspaceID + ":" + s.scope.ProjectID, "m:" + s.scope.WorkspaceID + ":" + s.scope.ProjectID + ":" + s.scope.MonitorID}
	limits := [3]int{4, 2, 1}
	rates := [3]float64{4, 2, 0}
	global := g.global.refill(now, 32)
	if g.active >= 32 || global.tokens < 1 {
		return nil, false
	}
	var rows [3]gateRecord
	missing := 0
	for i, k := range keys {
		r, exists := g.records[k]
		if !exists {
			missing++
		}
		if r.active >= limits[i] {
			return nil, false
		}
		if i < 2 {
			r.bucket = r.bucket.refill(now, rates[i])
			if r.bucket.tokens < 1 {
				return nil, false
			}
		} else if !r.lastStart.IsZero() {
			next := r.lastStart.Add(time.Duration(s.interval) * time.Second)
			if r.nextAllowed.After(next) {
				next = r.nextAllowed
			}
			if now.Before(next) {
				return nil, false
			}
		}
		rows[i] = r
	}
	if len(g.records)+missing > maxGateRecords {
		return nil, false
	}
	global.tokens--
	g.global = global
	g.active++
	for i, k := range keys {
		r := rows[i]
		r.active++
		if i < 2 {
			r.bucket.tokens--
			r.retainUntil = now.Add(time.Second)
		} else {
			r.lastStart = now
			r.nextAllowed = now.Add(time.Duration(s.interval) * time.Second)
			r.retainUntil = now.Add(time.Hour)
		}
		g.records[k] = r
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			defer g.mu.Unlock()
			g.active--
			for _, k := range keys {
				r := g.records[k]
				r.active--
				g.records[k] = r
			}
		})
	}, true
}
