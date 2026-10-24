package main

import (
	"errors"
	"testing"
	"time"
)

func TestCached(t *testing.T) {
	calls := 0
	fail := false
	c := &cached[int]{ttl: time.Hour, fetch: func() (int, error) {
		if fail {
			return 0, errors.New("upstream down")
		}
		calls++
		return calls, nil
	}}
	if v, _ := c.get(); v != 1 {
		t.Fatalf("first get = %d, want 1", v)
	}
	if v, _ := c.get(); v != 1 || calls != 1 {
		t.Fatalf("get within ttl = %d after %d calls, want cached 1", v, calls)
	}

	c.fetchedAt = c.fetchedAt.Add(-time.Hour)
	fail = true
	if _, err := c.get(); err == nil {
		t.Fatal("get after ttl with a failing fetch: want error")
	}
	fail = false
	if v, _ := c.get(); v != 2 {
		t.Fatalf("get after ttl = %d, want refetched 2", v)
	}
}
