package adapter

import (
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeduper_IsNewer_Basic(t *testing.T) {
	t.Parallel()

	deduper := NewDeduper()
	key := "fp1"
	first := time.Now()
	later := first.Add(time.Nanosecond)
	earlier := first.Add(-time.Nanosecond)

	// First time for a key -> true
	assert.True(t, deduper.isNewer(key, first))

	// Equal timestamp -> false
	assert.False(t, deduper.isNewer(key, first))

	// Older timestamp -> false
	assert.False(t, deduper.isNewer(key, earlier))

	// UpdateIfNewer timestamp -> true
	assert.True(t, deduper.isNewer(key, later))

	// After newer is set, older again -> false
	assert.False(t, deduper.isNewer(key, first))
}

func TestDeduper_IsNewer_PerKeyIsolation(t *testing.T) {
	t.Parallel()

	deduper := NewDeduper()
	baseTime := time.Now()

	keyA := "fpA"
	keyB := "fpB"

	// Set keyA at +10ms
	at10ms := baseTime.Add(10 * time.Millisecond)
	assert.True(t, deduper.isNewer(keyA, at10ms))

	// Set keyB at +5ms (independent) -> true
	at5ms := baseTime.Add(5 * time.Millisecond)
	assert.True(t, deduper.isNewer(keyB, at5ms))

	// keyA older than last -> false
	at9ms := baseTime.Add(9 * time.Millisecond)
	assert.False(t, deduper.isNewer(keyA, at9ms))

	// keyA newer than last -> true
	at11ms := baseTime.Add(11 * time.Millisecond)
	assert.True(t, deduper.isNewer(keyA, at11ms))

	// keyB equal -> false
	assert.False(t, deduper.isNewer(keyB, at5ms))
}

func TestDeduper_ZeroTime(t *testing.T) {
	t.Parallel()

	deduper := NewDeduper()
	key := "zero-time-key"

	// First zero -> true (we accept zero as "first seen")
	assert.True(t, deduper.isNewer(key, time.Time{}))

	// Second zero -> false
	assert.False(t, deduper.isNewer(key, time.Time{}))

	// Non-zero after zero -> true
	assert.True(t, deduper.isNewer(key, time.Now()))
}

func TestDeduper_Concurrent(t *testing.T) {
	t.Parallel()

	deduper := NewDeduper()
	key := "concurrent-key"

	const numUpdates = 1000
	start := time.Now().Truncate(time.Microsecond)

	timestamps := make([]time.Time, numUpdates)
	for i := range numUpdates {
		timestamps[i] = start.Add(time.Duration(i) * time.Nanosecond)
	}

	randomOrder := rand.New(rand.NewSource(42)).Perm(numUpdates) //nolint:gosec

	var waitGroup sync.WaitGroup
	waitGroup.Add(numUpdates)
	for _, index := range randomOrder {
		ts := timestamps[index]
		go func(ts time.Time) {
			defer waitGroup.Done()
			_, _ = deduper.UpdateIfNewer(key, ts)
		}(ts)
	}
	waitGroup.Wait()

	// Final stored value must be the max (last element of timestamps).
	lastSeen, exists := deduper.PeekLast(key)

	require.True(t, exists, "expected key to be present after updates")
	require.Equal(t, timestamps[numUpdates-1], lastSeen)
}

func TestDeduper_Concurrent_MultipleKeys(t *testing.T) {
	deduper := NewDeduper()
	const n = 500
	base := time.Unix(0, 0)

	keys := []string{"k1", "k2", "k3"}
	var wg sync.WaitGroup
	wg.Add(len(keys) * n)

	for _, k := range keys {
		// different permutations per key
		perm := rand.New(rand.NewSource(int64(len(k)))).Perm(n) //nolint:gosec
		for _, idx := range perm {
			ts := base.Add(time.Duration(idx) * time.Nanosecond)
			go func(key string, ts time.Time) {
				defer wg.Done()
				_, _ = deduper.UpdateIfNewer(key, ts)
			}(k, ts)
		}
	}
	wg.Wait()

	for _, k := range keys {
		lastSeen, ok := deduper.PeekLast(k)
		require.True(t, ok)
		require.Equal(t, base.Add(time.Duration(n-1)*time.Nanosecond), lastSeen)
	}
}
