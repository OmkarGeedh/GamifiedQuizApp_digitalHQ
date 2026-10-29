package cache

import (
	"sync"
	"time"
)

// Local is an in-process TTL cache for hot, rarely-changing lookups (e.g. the auth
// middleware's client check). It costs no network round trip, so it also works when
// Redis is absent. Entries are per instance; keep TTLs short.
var Local localCache

type localCache struct{ m sync.Map }

type localEntry struct {
	value   any
	expires time.Time
}

func (c *localCache) Get(key string) (any, bool) {
	v, ok := c.m.Load(key)
	if !ok {
		return nil, false
	}
	e := v.(localEntry)
	if time.Now().After(e.expires) {
		c.m.Delete(key)
		return nil, false
	}
	return e.value, true
}

func (c *localCache) Set(key string, value any, ttl time.Duration) {
	c.m.Store(key, localEntry{value: value, expires: time.Now().Add(ttl)})
}

func (c *localCache) Delete(key string) {
	c.m.Delete(key)
}
