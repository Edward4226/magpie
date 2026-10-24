package provider

import (
	"container/list"
	"testing"
	"testing/synctest"
)

// A test's session-id burst must not leave its millisecond's counter for
// the next call (#3284). Frozen in one millisecond, the bounded-cache test
// runs first, then a stateless call makes the ID it would make after it.
func TestOpenCodeBoundedBurstLeavesSessionIDs(t *testing.T) {
	openCodeIDs.Lock()
	last, counter := openCodeIDs.last, openCodeIDs.counter
	sessions, order := openCodeIDs.sessions, openCodeIDs.order
	openCodeIDs.last, openCodeIDs.counter = 0, 0
	openCodeIDs.sessions, openCodeIDs.order = map[string]*list.Element{}, list.New()
	openCodeIDs.Unlock()
	defer func() {
		openCodeIDs.Lock()
		openCodeIDs.last, openCodeIDs.counter = last, counter
		openCodeIDs.sessions, openCodeIDs.order = sessions, order
		openCodeIDs.Unlock()
	}()

	synctest.Test(t, func(t *testing.T) {
		TestOpenCodeSessionsKeptBounded(t)
		if id := openCodeSession(""); !openCodeRealSession(id) {
			t.Fatalf("the burst left %q invalid", id)
		}
	})
}
