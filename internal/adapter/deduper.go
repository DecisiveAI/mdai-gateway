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

// UpdateIfNewer is the post-publish commit paired with PeekLast at adaptation time:
// a failed publish leaves the alert eligible for Alertmanager's retry. Concurrent
// deliveries may both pass the peek and publish twice; duplicates are preferred over
// dropping an alert.
func (d *Deduper) UpdateIfNewer(key string, changeTime time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if prev, ok := d.last[key]; ok && isStale(changeTime, prev) {
		return false
	}
	d.last[key] = changeTime
	return true
}

func isStale(changeTime, last time.Time) bool { return !changeTime.After(last) }

func (d *Deduper) PeekLast(key string) (time.Time, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	t, ok := d.last[key]
	return t, ok
}
