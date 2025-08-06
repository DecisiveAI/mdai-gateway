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

func (d *Deduper) IsNewer(fp string, t time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if prev, ok := d.last[fp]; ok && !t.After(prev) {
		return false
	}
	d.last[fp] = t
	return true
}
