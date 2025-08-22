package adapter

import (
	"sync"
	"time"
)

type Deduper struct {
	mu   sync.Mutex
	last map[string]time.Time // TODO add TTL, default 12h equal to the Alertmanager configuration
}

func NewDeduper() *Deduper { return &Deduper{last: make(map[string]time.Time)} }

// UpdateIfNewer checks the stored time for key and, if changeTime is strictly newer.
func (d *Deduper) UpdateIfNewer(fingerprint string, changeTime time.Time) (bool, time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if prev, ok := d.last[fingerprint]; ok && !changeTime.After(prev) {
		return false, prev
	}
	d.last[fingerprint] = changeTime
	return true, changeTime
}

func (d *Deduper) PeekLast(key string) (time.Time, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	t, ok := d.last[key]
	return t, ok
}

func (d *Deduper) isNewer(fingerprint string, changeTime time.Time) bool {
	updated, _ := d.UpdateIfNewer(fingerprint, changeTime)
	return updated
}
