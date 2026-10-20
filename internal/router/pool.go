package router

import (
	"fmt"
	"log"
	"sort"
	"sync"

	"socks5-pool/internal/model"
)

// ProxyPool holds a list of verified proxies sorted by speed.
// Always keeps the fastest working proxy at index 0.
type ProxyPool struct {
	mu       sync.RWMutex
	proxies  []model.Proxy
	current  int
	logStore *LogStore
}

func NewProxyPool(logStore *LogStore) *ProxyPool {
	return &ProxyPool{
		logStore: logStore,
	}
}

func (p *ProxyPool) log(msg, logType string) {
	if p.logStore != nil {
		p.logStore.Add(msg, logType)
	}
}

// AddProxies merges incoming verified proxies from external harvesters into the pool,
// eliminating duplicates and keeping the pool sorted by latency.
func (p *ProxyPool) AddProxies(newProxies []model.Proxy) (added int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	existing := make(map[string]int)
	for i, px := range p.proxies {
		existing[px.Addr()] = i
	}

	for _, np := range newProxies {
		if np.IP == "" || np.Port == "" {
			continue
		}
		if idx, found := existing[np.Addr()]; found {
			if np.Latency > 0 {
				p.proxies[idx].Latency = np.Latency
			}
			if np.Country != "" && p.proxies[idx].Country == "" {
				p.proxies[idx].Country = np.Country
				p.proxies[idx].City = np.City
				p.proxies[idx].CountryCode = np.CountryCode
			}
		} else {
			existing[np.Addr()] = len(p.proxies)
			p.proxies = append(p.proxies, np)
			added++
		}
	}

	if len(p.proxies) > 0 {
		sort.Slice(p.proxies, func(i, j int) bool {
			return p.proxies[i].Latency < p.proxies[j].Latency
		})
	}

	if added > 0 {
		log.Printf("[pool] feed accepted: +%d new proxies (total: %d)", added, len(p.proxies))
		p.log(fmt.Sprintf("Harvester feed: added %d proxies (pool total: %d)", added, len(p.proxies)), "refresh")
	}
	return added
}

// Update replaces the proxy list with new verified proxies (already sorted by latency).
// Resets current to 0 (fastest proxy).
func (p *ProxyPool) Update(proxies []model.Proxy) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.proxies = proxies
	p.current = 0
	if len(proxies) > 0 {
		log.Printf("[pool] active fastest proxy: %s (%s, %s) [%d ms]", proxies[0].Addr(), proxies[0].Country, proxies[0].City, proxies[0].Latency.Milliseconds())
		p.log(fmt.Sprintf("Pool refreshed: %d live proxies. Selected fastest #0 (%s, %d ms)", len(proxies), proxies[0].Addr(), proxies[0].Latency.Milliseconds()), "refresh")
	}
}

// Current returns the current active proxy (index 0 unless manually overridden).
func (p *ProxyPool) Current() (model.Proxy, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.proxies) == 0 {
		return model.Proxy{}, false
	}
	if p.current >= len(p.proxies) {
		p.current = 0
	}
	return p.proxies[p.current], true
}

// RemoveFailed removes a non-working proxy and ensures current points to the fastest remaining proxy.
func (p *ProxyPool) RemoveFailed(addr string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, px := range p.proxies {
		if px.Addr() == addr {
			log.Printf("[pool] pruned non-working proxy: %s (%s, %d ms)", addr, px.Country, px.Latency.Milliseconds())
			p.proxies = append(p.proxies[:i], p.proxies[i+1:]...)
			p.current = 0
			if len(p.proxies) > 0 {
				log.Printf("[pool] promoted new fastest active proxy: %s (%s, %d ms)", p.proxies[0].Addr(), p.proxies[0].Country, p.proxies[0].Latency.Milliseconds())
			}
			return
		}
	}
	p.current = 0
}

// ResetToFastest resets active proxy to index 0 (fastest).
func (p *ProxyPool) ResetToFastest() (model.Proxy, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.proxies) == 0 {
		return model.Proxy{}, false
	}
	p.current = 0
	px := p.proxies[0]
	log.Printf("[pool] reset to fastest: %s (%s) [%d ms]", px.Addr(), px.Country, px.Latency.Milliseconds())
	return px, true
}

// SwitchNext switches to next proxy (for manual WebUI rotation).
func (p *ProxyPool) SwitchNext() (model.Proxy, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.proxies) == 0 {
		return model.Proxy{}, false
	}
	p.current = (p.current + 1) % len(p.proxies)
	px := p.proxies[p.current]
	log.Printf("[pool] manual switch to: %s (%s) [%d ms]", px.Addr(), px.Country, px.Latency.Milliseconds())
	return px, true
}

// SwitchTo switches to a specific proxy by index (from WebUI).
func (p *ProxyPool) SwitchTo(index int) (model.Proxy, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if index < 0 || index >= len(p.proxies) {
		return model.Proxy{}, false
	}
	p.current = index
	px := p.proxies[p.current]
	log.Printf("[pool] manual switch to #%d: %s (%s) [%d ms]", index, px.Addr(), px.Country, px.Latency.Milliseconds())
	return px, true
}

func (p *ProxyPool) CurrentIndex() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.current
}

func (p *ProxyPool) Size() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.proxies)
}

func (p *ProxyPool) All() []model.Proxy {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]model.Proxy, len(p.proxies))
	copy(result, p.proxies)
	return result
}
