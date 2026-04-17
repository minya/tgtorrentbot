package internal

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

const expirationDuration = 60 * 60 // 1 hour in seconds

type lookupRecord struct {
	id        uuid.UUID
	value     string
	timestamp int64
}

type FileIDLookup struct {
	mu    sync.Mutex
	items []lookupRecord
}

func (lookup *FileIDLookup) Add(value string) string {
	newID := uuid.New()
	lookup.mu.Lock()
	defer lookup.mu.Unlock()
	lookup.cleanupLocked()
	lookup.items = append(lookup.items, lookupRecord{id: newID, value: value, timestamp: time.Now().Unix()})
	return newID.String()
}

func (lookup *FileIDLookup) Get(id string) (string, bool) {
	idUUID, err := uuid.Parse(id)
	if err != nil {
		return "", false
	}
	lookup.mu.Lock()
	defer lookup.mu.Unlock()
	for _, record := range lookup.items {
		if record.id == idUUID {
			return record.value, true
		}
	}
	return "", false
}

func (lookup *FileIDLookup) cleanupLocked() {
	cutoff := time.Now().Unix() - expirationDuration
	valid := lookup.items[:0]
	for _, record := range lookup.items {
		if record.timestamp >= cutoff {
			valid = append(valid, record)
		}
	}
	for i := len(valid); i < len(lookup.items); i++ {
		lookup.items[i] = lookupRecord{}
	}
	lookup.items = valid
}
