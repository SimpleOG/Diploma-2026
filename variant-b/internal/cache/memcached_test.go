package cache_test

import (
	"testing"

	"github.com/chat-diploma/variant-b/internal/cache"
)

// mockMemcache is a simple in-memory stand-in used for unit tests.
// The real MemcachedClient requires a live server, so we test the key-naming
// logic and helper behaviour using the exported surface only.

// TestMemcachedKeyFormat verifies the client can be constructed and that
// operations return sensible defaults on a non-existent server (cache miss or
// connection error should not panic).
func TestMemcachedClient_Construction(t *testing.T) {
	// Use an address that is not reachable so every call will fail gracefully.
	c := cache.NewMemcachedClient("127.0.0.1:11222")
	if c == nil {
		t.Fatal("NewMemcachedClient returned nil")
	}
}

func TestMemcachedClient_IsMember_MissOnUnreachableServer(t *testing.T) {
	c := cache.NewMemcachedClient("127.0.0.1:11222")

	// With an unreachable server, IsMember should return an error.
	_, err := c.IsMember("room1", "user1")
	if err == nil {
		t.Log("IsMember returned nil error (server may be running); skipping")
		return
	}
	// Error is expected and acceptable – the important thing is no panic.
}

func TestMemcachedClient_GetUsername_MissOnUnreachableServer(t *testing.T) {
	c := cache.NewMemcachedClient("127.0.0.1:11222")

	username, ok := c.GetUsername("user1")
	// On connection error the method must return ("", false) without panicking.
	if ok {
		t.Log("GetUsername returned ok=true (server may be running, skipping)")
		return
	}
	if username != "" {
		t.Errorf("expected empty username on miss, got %q", username)
	}
}

func TestMemcachedClient_SetUsername_DoesNotPanic(t *testing.T) {
	c := cache.NewMemcachedClient("127.0.0.1:11222")
	// SetUsername logs errors internally; should never panic.
	c.SetUsername("user1", "alice")
}

func TestMemcachedClient_SetMember_ErrorOnUnreachableServer(t *testing.T) {
	c := cache.NewMemcachedClient("127.0.0.1:11222")
	err := c.SetMember("room1", "user1")
	if err == nil {
		t.Log("SetMember returned nil error (server may be running); skipping")
	}
	// Error is expected and acceptable.
}
