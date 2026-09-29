package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

// Blocked countries: unsupported by Google / OpenAI / Anthropic
var blockedCountries = map[string]bool{
	"china":       true,
	"cn":          true,
	"hong kong":   true,
	"hk":          true,
	"russia":      true,
	"ru":          true,
	"belarus":     true,
	"by":          true,
	"iran":        true,
	"ir":          true,
	"north korea": true,
	"kp":          true,
}

func isCountryBlocked(country, code string) bool {
	if country != "" && blockedCountries[strings.ToLower(strings.TrimSpace(country))] {
		return true
	}
	if code != "" && blockedCountries[strings.ToLower(strings.TrimSpace(code))] {
		return true
	}
	return false
}

// CheckProxies concurrently checks proxies for HTTPS (port 443 + TLS) connectivity.
// Filters out blocked countries, measures real latency, and sorts by speed.
func CheckProxies(proxies []Proxy, timeout time.Duration, maxLatency time.Duration, maxConcurrent int) []Proxy {
	var (
		mu    sync.Mutex
		alive []Proxy
		wg    sync.WaitGroup
		sem   = make(chan struct{}, maxConcurrent)
	)

	// Cap candidates to test per cycle to keep check time under 10-15s
	maxCandidates := 100
	if len(proxies) > maxCandidates {
		proxies = proxies[:maxCandidates]
	}

	for _, p := range proxies {
		// Fast skip if known country is blocked
		if isCountryBlocked(p.Country, p.CountryCode) {
			continue
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(px Proxy) {
			defer wg.Done()
			defer func() { <-sem }()

			// 1. Measure real HTTPS (port 443 + TLS) latency
			ok, latency := checkHTTPS(px, timeout)
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

			if isCountryBlocked(px.Country, px.CountryCode) {
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

// checkHTTPS connects through the proxy to www.google.com:443 and performs a TLS handshake.
// This guarantees the proxy supports HTTPS/port 443 required by OpenAI and Zed.
func checkHTTPS(p Proxy, timeout time.Duration) (bool, time.Duration) {
	start := time.Now()

	conn, err := net.DialTimeout("tcp", p.Addr(), timeout)
	if err != nil {
		return false, 0
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(timeout))

	// SOCKS5 greeting
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return false, 0
	}
	buf := make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err != nil || buf[0] != 0x05 {
		return false, 0
	}

	// Connect to www.google.com:443 through proxy
	target := "www.google.com"
	req := []byte{0x05, 0x01, 0x00, 0x03, byte(len(target))}
	req = append(req, []byte(target)...)
	req = append(req, 0x01, 0xbb) // port 443 (0x01bb)

	if _, err := conn.Write(req); err != nil {
		return false, 0
	}

	resp := make([]byte, 256)
	n, err := conn.Read(resp)
	if err != nil || n < 2 || resp[1] != 0x00 {
		return false, 0
	}

	// Perform TLS handshake to guarantee that HTTPS / port 443 is working
	tlsConn := tls.Client(conn, &tls.Config{
		ServerName: "www.google.com",
	})
	tlsConn.SetDeadline(time.Now().Add(timeout))
	if err := tlsConn.Handshake(); err != nil {
		return false, 0
	}

	return true, time.Since(start)
}

// LookupGeo queries ip-api.com for IP geolocation (only called for live proxies without geo info).
func LookupGeo(ip string, timeout time.Duration) (country, city, countryCode string) {
	conn, err := net.DialTimeout("tcp", "ip-api.com:80", timeout)
	if err != nil {
		return "Unknown", "", ""
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(timeout))

	req := fmt.Sprintf("GET /csv/%s?fields=country,city,countryCode HTTP/1.1\r\nHost: ip-api.com\r\nConnection: close\r\n\r\n", ip)
	conn.Write([]byte(req))

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil || n == 0 {
		return "Unknown", "", ""
	}

	body := string(buf[:n])
	if idx := strings.Index(body, "\r\n\r\n"); idx != -1 {
		body = strings.TrimSpace(body[idx+4:])
	}
	parts := strings.Split(body, ",")
	if len(parts) >= 3 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2])
	} else if len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), ""
	} else if len(parts) == 1 {
		return strings.TrimSpace(parts[0]), "", ""
	}
	return "Unknown", "", ""
}
