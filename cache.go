package main

import (
	"sync"
	"time"
)

// cached memoizes fetch for ttl. The age counts from when a fetch starts, so a client polling at exactly ttl gets fresh data each time.
type cached[T any] struct {
	ttl   time.Duration
	fetch func() (T, error)

	mu        sync.Mutex
	value     T
	fetchedAt time.Time
}

func (c *cached[T]) get() (T, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if start := time.Now(); start.Sub(c.fetchedAt) >= c.ttl {
		v, err := c.fetch()
		if err != nil {
			return v, err
		}
		c.value, c.fetchedAt = v, start
	}
	return c.value, nil
}
