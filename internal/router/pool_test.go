package router

import (
	"testing"
	"time"

	"socks5-pool/internal/model"
)

func TestProxyPool(t *testing.T) {
	pool := NewProxyPool(nil)

	proxies := []model.Proxy{
		{IP: "1.1.1.1", Port: "1080", Latency: 100 * time.Millisecond},
		{IP: "2.2.2.2", Port: "1080", Latency: 200 * time.Millisecond},
		{IP: "3.3.3.3", Port: "1080", Latency: 300 * time.Millisecond},
	}

	pool.Update(proxies)
	if pool.Size() != 3 {
		t.Fatalf("expected size 3, got %d", pool.Size())
	}

	// Fastest should be current
	curr, ok := pool.Current()
	if !ok || curr.IP != "1.1.1.1" {
		t.Errorf("expected 1.1.1.1, got %v", curr)
	}

	// Manual switch next
	next, ok := pool.SwitchNext()
	if !ok || next.IP != "2.2.2.2" {
		t.Errorf("expected 2.2.2.2, got %v", next)
	}

	// Remove non-working proxy
	pool.RemoveFailed("1.1.1.1:1080")
	if pool.Size() != 2 {
		t.Errorf("expected size 2, got %d", pool.Size())
	}
}
