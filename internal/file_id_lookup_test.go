package internal

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestFileIDLookupAddGet(t *testing.T) {
	var l FileIDLookup
	id := l.Add("file-id-1")
	if len(id) != 36 {
		t.Fatalf("key %q is not a 36-char UUID", id)
	}
	if v, ok := l.Get(id); !ok || v != "file-id-1" {
		t.Fatalf("Get = %q, %v", v, ok)
	}
	for _, bad := range []string{"", "not-a-uuid", "00000000-0000-0000-0000-000000000000"} {
		if _, ok := l.Get(bad); ok {
			t.Errorf("Get(%q) found a value", bad)
		}
	}
}

// Entries survive until their hour is up and are evicted by the next Add
// after that.
func TestFileIDLookupExpiry(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	l := FileIDLookup{now: func() time.Time { return now }}
	old := l.Add("old")

	now = now.Add(time.Hour) // exactly one hour: still valid
	l.Add("trigger cleanup")
	if _, ok := l.Get(old); !ok {
		t.Fatal("entry evicted at exactly one hour")
	}

	now = now.Add(time.Second)
	fresh := l.Add("fresh")
	if _, ok := l.Get(old); ok {
		t.Fatal("entry older than one hour was not evicted")
	}
	if v, ok := l.Get(fresh); !ok || v != "fresh" {
		t.Fatal("fresh entry lost")
	}
	if n := len(l.items); n != 2 {
		t.Fatalf("lookup holds %d entries, want 2", n)
	}
}

// Add and Get are called concurrently from webhook goroutines; run with -race.
func TestFileIDLookupConcurrent(t *testing.T) {
	var l FileIDLookup
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range 200 {
				v := fmt.Sprintf("%d-%d", i, j)
				id := l.Add(v)
				if got, ok := l.Get(id); !ok || got != v {
					t.Errorf("Get after Add = %q, %v; want %q", got, ok, v)
					return
				}
			}
		}()
	}
	wg.Wait()
}
