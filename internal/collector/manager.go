package collector

import (
	"context"
	"log"
	"net/http"
	"time"

	"socks5-pool/internal/model"
)

// Manager coordinates fetching and aggregating proxies across multiple providers.
type Manager struct {
	client    *http.Client
	providers []Provider
}

// NewManager creates a proxy collection manager.
func NewManager(client *http.Client) *Manager {
	if client == nil {
		t := &http.Transport{}
		t.RegisterProtocol("file", http.NewFileTransport(http.Dir("/")))
		client = &http.Client{
			Transport: t,
			Timeout:   15 * time.Second,
		}
	}
	return &Manager{
		client:    client,
		providers: make([]Provider, 0),
	}
}

// RegisterProvider registers an upstream proxy provider into the manager.
func (m *Manager) RegisterProvider(p Provider) {
	m.providers = append(m.providers, p)
}

// RegisterDefaultProviders sets up the standard default sources, prioritizing verified and fresh feeds.
func (m *Manager) RegisterDefaultProviders() {
	// High-priority verified / live feeds
	m.RegisterProvider(NewCharlesPikachuProvider())
	m.RegisterProvider(NewProxifyProvider())
	m.RegisterProvider(NewTextURLProvider("yakumo-checked", "https://raw.githubusercontent.com/elliottophellia/yakumo/master/results/socks5/global/socks5_checked.txt"))
	m.RegisterProvider(NewTextURLProvider("proxyscrape", "https://api.proxyscrape.com/v2/?request=displayproxies&protocol=socks5&timeout=3000&country=all"))
	m.RegisterProvider(NewTextURLProvider("hookzof", "https://raw.githubusercontent.com/hookzof/socks5_list/master/proxy.txt"))
	m.RegisterProvider(NewTextURLProvider("proxifly", "https://raw.githubusercontent.com/proxifly/free-proxy-list/main/proxies/protocols/socks5/data.txt"))
	m.RegisterProvider(NewTextURLProvider("vakhov", "https://raw.githubusercontent.com/vakhov/fresh-proxy-list/master/socks5.txt"))
	m.RegisterProvider(NewTextURLProvider("prxchk", "https://raw.githubusercontent.com/prxchk/proxy-list/main/socks5.txt"))
	// High-volume bulk lists
	m.RegisterProvider(NewTextURLProvider("monosans", "https://raw.githubusercontent.com/monosans/proxy-list/main/proxies/socks5.txt"))
	m.RegisterProvider(NewTextURLProvider("thespeedx", "https://raw.githubusercontent.com/TheSpeedX/PROXY-List/master/socks5.txt"))
	m.RegisterProvider(NewTextURLProvider("socks5-proxy.github.io", "https://socks5-proxy.github.io/"))
}

// Collect gathers proxies from all registered providers or a custom URL.
func (m *Manager) Collect(ctx context.Context, customSource string) ([]model.Proxy, error) {
	// If custom source URL or local file was passed
	if customSource != "" && customSource != "auto" && customSource != "default" {
		provider := NewTextURLProvider("custom", customSource)
		return provider.Fetch(ctx, m.client)
	}

	log.Printf("[collector] starting multi-source collection from %d providers...", len(m.providers))
	var all []model.Proxy
	seen := make(map[string]bool)

	for _, p := range m.providers {
		proxies, err := p.Fetch(ctx, m.client)
		if err != nil {
			log.Printf("[collector] provider %s failed: %v", p.Name(), err)
			continue
		}

		added := 0
		for _, px := range proxies {
			if !seen[px.Addr()] {
				seen[px.Addr()] = true
				all = append(all, px)
				added++
			}
		}
		log.Printf("[collector] %s: loaded %d proxies (+%d unique)", p.Name(), len(proxies), added)
	}

	log.Printf("[collector] total unique candidates collected: %d", len(all))
	return all, nil
}
