package cache

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/bradfitz/gomemcache/memcache"
)

const (
	memberTTL   = 300 // seconds
	usernameTTL = 600 // seconds
)

// MemcachedClient is a thin wrapper around the gomemcache client that provides
// domain-specific helpers for membership and username caching.
type MemcachedClient struct {
	mc *memcache.Client
}

// NewMemcachedClient creates a new MemcachedClient connected to addr.
func NewMemcachedClient(addr string) *MemcachedClient {
	mc := memcache.New(addr)
	return &MemcachedClient{mc: mc}
}

// IsMember returns true if the membership key is present in Memcached.
// A cache miss (ErrCacheMiss) returns (false, nil).
func (c *MemcachedClient) IsMember(roomID, userID string) (bool, error) {
	key := fmt.Sprintf("member:%s:%s", roomID, userID)
	_, err := c.mc.Get(key)
	if err != nil {
		if errors.Is(err, memcache.ErrCacheMiss) {
			return false, nil
		}
		return false, fmt.Errorf("memcached: IsMember: %w", err)
	}
	return true, nil
}

// SetMember stores a membership marker with a TTL of 300 seconds.
func (c *MemcachedClient) SetMember(roomID, userID string) error {
	key := fmt.Sprintf("member:%s:%s", roomID, userID)
	item := &memcache.Item{
		Key:        key,
		Value:      []byte("1"),
		Expiration: memberTTL,
	}
	if err := c.mc.Set(item); err != nil {
		return fmt.Errorf("memcached: SetMember: %w", err)
	}
	return nil
}

// GetUsername returns the cached username for userID.
// Returns ("", false) on cache miss without logging an error.
func (c *MemcachedClient) GetUsername(userID string) (string, bool) {
	key := fmt.Sprintf("user:%s:name", userID)
	item, err := c.mc.Get(key)
	if err != nil {
		if !errors.Is(err, memcache.ErrCacheMiss) {
			slog.Warn("memcached: GetUsername error", "userID", userID, "error", err)
		}
		return "", false
	}
	return string(item.Value), true
}

// SetUsername stores a username with a TTL of 600 seconds.
func (c *MemcachedClient) SetUsername(userID, username string) {
	key := fmt.Sprintf("user:%s:name", userID)
	item := &memcache.Item{
		Key:        key,
		Value:      []byte(username),
		Expiration: usernameTTL,
	}
	if err := c.mc.Set(item); err != nil {
		slog.Warn("memcached: SetUsername error", "userID", userID, "error", err)
	}
}
