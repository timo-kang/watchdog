package supervisor

import (
	"os"
	"sort"
	"strings"
	"sync"
)

type recentIDs struct {
	mu    sync.Mutex
	set   map[string]struct{}
	order []string
	cap   int
}

func newRecentIDs(capacity int) *recentIDs {
	if capacity < 1 {
		capacity = 1
	}
	return &recentIDs{set: make(map[string]struct{}, capacity), cap: capacity}
}

func (r *recentIDs) seen(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.set[id]
	return ok
}

func (r *recentIDs) add(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.set[id]; ok {
		return
	}
	r.set[id] = struct{}{}
	r.order = append(r.order, id)
	for len(r.order) > r.cap {
		oldest := r.order[0]
		r.order = r.order[1:]
		delete(r.set, oldest)
	}
}

// seedRecentIDs loads the newest audit request IDs (filename without .json) so
// duplicate suppression survives a restart even as retention prunes old files.
func seedRecentIDs(auditDir string, capacity int) *recentIDs {
	r := newRecentIDs(capacity)
	entries, err := os.ReadDir(auditDir)
	if err != nil {
		return r
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names) // oldest first; timestamp-prefixed
	// Seed the newest `capacity` in chronological order so eviction order matches.
	if len(names) > capacity {
		names = names[len(names)-capacity:]
	}
	for _, n := range names {
		r.add(strings.TrimSuffix(n, ".json"))
	}
	return r
}
