package collector

import (
	"context"
	"net/http"
	"testing"

	"socks5-pool/internal/model"
)

type mockProvider struct {
	name    string
	proxies []model.Proxy
}

func (m *mockProvider) Name() string { return m.name }
func (m *mockProvider) Fetch(ctx context.Context, client *http.Client) ([]model.Proxy, error) {
	return m.proxies, nil
}

func TestManagerCollect(t *testing.T) {
	mgr := NewManager(nil)
	mgr.RegisterProvider(&mockProvider{
		name: "source1",
		proxies: []model.Proxy{
			{IP: "1.1.1.1", Port: "1080"},
			{IP: "2.2.2.2", Port: "1080"},
		},
	})
	mgr.RegisterProvider(&mockProvider{
		name: "source2",
		proxies: []model.Proxy{
			{IP: "1.1.1.1", Port: "1080"}, // duplicate
			{IP: "3.3.3.3", Port: "1080"},
		},
	})

	proxies, err := mgr.Collect(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(proxies) != 3 {
		t.Errorf("expected 3 unique proxies, got %d", len(proxies))
	}
}
