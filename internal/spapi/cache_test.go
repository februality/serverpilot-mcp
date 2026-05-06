package spapi

import (
	"testing"
	"time"
)

func TestTTLCache_SetGet(t *testing.T) {
	c := NewTTLCache(60)
	c.Set("k", "v")
	v, ok := c.Get("k")
	if !ok || v != "v" {
		t.Fatalf("expected 'v', got %v ok=%v", v, ok)
	}
}

func TestTTLCache_Miss(t *testing.T) {
	c := NewTTLCache(60)
	if _, ok := c.Get("absent"); ok {
		t.Fatal("expected miss")
	}
}

func TestTTLCache_Expiry(t *testing.T) {
	c := NewTTLCache(0)
	c.Set("k", "v")
	c.store["k"] = cacheEntry{value: "v", expiresAt: time.Now().Add(-time.Second)}
	if _, ok := c.Get("k"); ok {
		t.Fatal("expected expired entry to miss")
	}
	if _, present := c.store["k"]; present {
		t.Fatal("expected expired entry to be deleted")
	}
}

func TestTTLCache_Invalidate(t *testing.T) {
	c := NewTTLCache(60)
	c.Set("k", "v")
	c.Invalidate("k")
	if _, ok := c.Get("k"); ok {
		t.Fatal("expected invalidated key to miss")
	}
}

func TestTTLCache_InvalidatePrefix(t *testing.T) {
	c := NewTTLCache(60)
	c.Set("app:1", 1)
	c.Set("app:2", 2)
	c.Set("server:1", 3)
	c.InvalidatePrefix("app")
	if _, ok := c.Get("app:1"); ok {
		t.Fatal("app:1 should be gone")
	}
	if _, ok := c.Get("app:2"); ok {
		t.Fatal("app:2 should be gone")
	}
	if _, ok := c.Get("server:1"); !ok {
		t.Fatal("server:1 should remain")
	}
}

func TestTTLCache_Clear(t *testing.T) {
	c := NewTTLCache(60)
	c.Set("a", 1)
	c.Set("b", 2)
	c.Clear()
	if _, ok := c.Get("a"); ok {
		t.Fatal("a should be cleared")
	}
}
