package model

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Proxy represents an upstream SOCKS5 proxy candidate.
type Proxy struct {
	IP          string        `json:"ip"`
	Port        string        `json:"port"`
	Country     string        `json:"country"`
	City        string        `json:"city"`
	CountryCode string        `json:"country_code"`
	Latency     time.Duration `json:"latency"`
}

func (p Proxy) Addr() string {
	return p.IP + ":" + p.Port
}

func (p Proxy) String() string {
	return fmt.Sprintf("socks5://%s:%s", p.IP, p.Port)
}

// ProxyStatus is the view model used in JSON API and Web Dashboard.
type ProxyStatus struct {
	Index       int    `json:"index"`
	Addr        string `json:"addr"`
	IP          string `json:"ip"`
	Port        string `json:"port"`
	Country     string `json:"country"`
	CountryCode string `json:"country_code"`
	City        string `json:"city"`
	Active      bool   `json:"active"`
	LatencyMs   int64  `json:"latency_ms"`
}

// ActivityLog represents a recent event in the router.
type ActivityLog struct {
	Time    string `json:"time"`
	Message string `json:"message"`
	Type    string `json:"type"` // "switch", "refresh", "test", "watchdog", "system"
}

// LatencyFilter limits API/export results without changing the live proxy pool.
// A max value is exclusive, matching the dashboard's latency bands:
// fast <500ms, normal 500-1199ms, and standard >=1200ms.
type LatencyFilter struct {
	MinMs   int64
	MaxMs   int64
	Country string
}

func (f LatencyFilter) Match(p Proxy) bool {
	latencyMs := p.Latency.Milliseconds()
	if f.MinMs > 0 && latencyMs < f.MinMs {
		return false
	}
	if f.MaxMs > 0 && latencyMs >= f.MaxMs {
		return false
	}
	if f.Country != "" {
		c := strings.ToLower(f.Country)
		matchCountry := strings.ToLower(p.Country) == c || strings.ToLower(p.CountryCode) == c
		if !matchCountry {
			return false
		}
	}
	return true
}

func (f LatencyFilter) Apply(proxies []Proxy) []Proxy {
	if f.MinMs == 0 && f.MaxMs == 0 && f.Country == "" {
		return proxies
	}

	filtered := make([]Proxy, 0, len(proxies))
	for _, p := range proxies {
		if f.Match(p) {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

func ParseLatencyFilter(r *http.Request) (LatencyFilter, error) {
	var filter LatencyFilter
	for name, target := range map[string]*int64{
		"min_latency_ms": &filter.MinMs,
		"max_latency_ms": &filter.MaxMs,
	} {
		value := strings.TrimSpace(r.URL.Query().Get(name))
		if value == "" {
			continue
		}

		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed <= 0 {
			return LatencyFilter{}, fmt.Errorf("%s must be a positive integer", name)
		}
		*target = parsed
	}

	if filter.MinMs > 0 && filter.MaxMs > 0 && filter.MinMs >= filter.MaxMs {
		return LatencyFilter{}, fmt.Errorf("min_latency_ms must be lower than max_latency_ms")
	}

	filter.Country = strings.TrimSpace(r.URL.Query().Get("country"))
	return filter, nil
}

// StatusData represents the full payload sent to dashboard and /api/status.
type StatusData struct {
	Total             int            `json:"total"`
	ActiveIndex       int            `json:"active_index"`
	ActiveProxy       string         `json:"active_proxy"`
	ActiveRegion      string         `json:"active_region"`
	ActiveCountry     string         `json:"active_country"`
	ActiveCity        string         `json:"active_city"`
	ActiveCountryCode string         `json:"active_country_code"`
	ActiveLatencyMs   int64          `json:"active_latency_ms"`
	FastestLatencyMs  int64          `json:"fastest_latency_ms"`
	AvgLatencyMs      int64          `json:"avg_latency_ms"`
	FastCount         int            `json:"fast_count"`
	MidCount          int            `json:"mid_count"`
	SlowCount         int            `json:"slow_count"`
	CountriesCount    int            `json:"countries_count"`
	LastScrape        string         `json:"last_scrape"`
	NextScrape        string         `json:"next_scrape"`
	NextScrapeUnix    int64          `json:"next_scrape_unix"`
	ListenAddr        string         `json:"listen_addr"`
	AuthUser          string         `json:"auth_user"`
	AuthPass          string         `json:"auth_pass"`
	FilterPolicy      string         `json:"filter_policy"`
	BlockedCountries  []string       `json:"blocked_countries"`
	CountryStats      map[string]int `json:"country_stats"`
	Logs              []ActivityLog  `json:"logs"`
	Proxies           []ProxyStatus  `json:"proxies"`
}
