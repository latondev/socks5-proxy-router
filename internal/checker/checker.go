package checker

import (
	"crypto/tls"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"socks5-pool/internal/model"
)

// CheckProxies concurrently checks proxies for HTTPS (port 443 + TLS) connectivity.
// Filters out blocked countries, measures real latency, and sorts by speed.
func CheckProxies(proxies []model.Proxy, timeout time.Duration, maxLatency time.Duration, maxConcurrent int) []model.Proxy {
	var (
		mu    sync.Mutex
		alive []model.Proxy
		wg    sync.WaitGroup
		sem   = make(chan struct{}, maxConcurrent)
	)

	// Cap candidates to test per cycle to keep check time under 10-15s
	maxCandidates := 150
	if len(proxies) > maxCandidates {
		proxies = proxies[:maxCandidates]
	}

	for _, p := range proxies {
		// Fast skip if known country is blocked
		if IsCountryBlocked(p.Country, p.CountryCode) {
			continue
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(px model.Proxy) {
			defer wg.Done()
			defer func() { <-sem }()

			// 1. Measure real HTTPS (port 443 + TLS) latency
			ok, latency := CheckHTTPS(px, timeout)
			if !ok {
				return
			}

			// Filter out if latency exceeds threshold
			if maxLatency > 0 && latency > maxLatency {
				return
			}

			px.Latency = latency

			// 2. If geo info is missing, lookup now (only for verified live proxies)
			if px.Country == "" {
				country, city, countryCode := LookupGeo(px.IP, timeout)
				px.Country = strings.TrimSpace(country)
				px.City = strings.TrimSpace(city)
				px.CountryCode = strings.TrimSpace(countryCode)
			}

			if IsCountryBlocked(px.Country, px.CountryCode) {
				return
			}

			mu.Lock()
			alive = append(alive, px)
			mu.Unlock()
		}(p)
	}

	wg.Wait()

	// Sort by latency ascending (fastest proxy at index 0)
	sort.Slice(alive, func(i, j int) bool {
		return alive[i].Latency < alive[j].Latency
	})

	log.Printf("[checker] %d/%d proxies alive & verified for HTTPS/TLS (sorted by latency)", len(alive), len(proxies))
	for i, px := range alive {
		if i < 5 {
			log.Printf("  #%d [%d ms] %s (%s, %s)", i+1, px.Latency.Milliseconds(), px.Addr(), px.Country, px.City)
		}
	}

	return alive
}

// CheckHTTPS connects through the proxy and verifies both api.ipify.org (external IP retrieval)
// and https://www.youtube.com/generate_204 (direct YouTube connectivity).
var ipv4Regex = regexp.MustCompile(`^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$`)

func CheckHTTPS(p model.Proxy, timeout time.Duration) (bool, time.Duration) {
	start := time.Now()

	proxyURL, err := url.Parse("socks5://" + p.Addr())
	if err != nil {
		return false, 0
	}

	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
		DisableKeepAlives: true,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}

	// 1. Verify api.ipify.org (ensures DNS resolution & outbound IPv4 traffic works)
	req1, err := http.NewRequest("GET", "https://api.ipify.org", nil)
	if err != nil {
		return false, 0
	}
	req1.Header.Set("User-Agent", "curl/8.4.0")
	resp1, err := client.Do(req1)
	if err != nil {
		return false, 0
	}
	defer resp1.Body.Close()

	body, err := io.ReadAll(resp1.Body)
	if err != nil {
		return false, 0
	}
	egressIP := strings.TrimSpace(string(body))
	if !ipv4Regex.MatchString(egressIP) {
		return false, 0
	}

	// 2. Verify https://www.youtube.com/generate_204 (ensures YouTube is not blocked)
	req2, err := http.NewRequest("GET", "https://www.youtube.com/generate_204", nil)
	if err != nil {
		return false, 0
	}
	req2.Header.Set("User-Agent", "curl/8.4.0")
	resp2, err := client.Do(req2)
	if err != nil {
		return false, 0
	}
	defer resp2.Body.Close()

	if resp2.StatusCode < 200 || resp2.StatusCode >= 400 {
		return false, 0
	}

	latency := time.Since(start)
	return true, latency
}
