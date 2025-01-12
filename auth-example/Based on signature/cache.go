package main

import (
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
)

type Cache struct {
	data map[string]cacheItem
	mu   sync.RWMutex
	ttl  time.Duration
}

type cacheItem struct {
	value      crypto.PubKey
	expiration time.Time
}

func NewCache(ttl time.Duration) *Cache {
	return &Cache{
		data: make(map[string]cacheItem),
		ttl:  ttl,
	}
}

func (c *Cache) Set(key string, value crypto.PubKey) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data[key] = cacheItem{
		value:      value,
		expiration: time.Now().Add(c.ttl),
	}
}

func (c *Cache) Get(key string) (crypto.PubKey, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	item, found := c.data[key]
	if !found || time.Now().After(item.expiration) {
		// Remove expired item
		delete(c.data, key)
		return nil, false
	}
	return item.value, true
}

func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.data, key)
}
