package checker

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

	// Cap candidates to test per cycle
	maxCandidates := 400
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

			// 1. Measure real HTTPS (port 443 + TLS + HTTP payload) latency
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

// CheckHTTPS connects through the proxy to cloud.zed.dev:443 (Cloudflare) and performs:
// 1. SOCKS5 connect
// 2. Strict TLS handshake with valid certificate verification (rejects MITM / self-signed proxies)
// 3. Real HTTP request and response reading (rejects zombie proxies that drop on payload)
func CheckHTTPS(p model.Proxy, timeout time.Duration) (bool, time.Duration) {
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

	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil || reply[0] != 0x05 || reply[1] != 0x00 {
		return false, 0
	}

	// SOCKS5 CONNECT to cloud.zed.dev:443
	target := "cloud.zed.dev"
	req := []byte{0x05, 0x01, 0x00, 0x03, byte(len(target))}
	req = append(req, []byte(target)...)
	req = append(req, 0x01, 0xbb) // port 443

	if _, err := conn.Write(req); err != nil {
		return false, 0
	}

	connReply := make([]byte, 256)
	n, err := conn.Read(connReply)
	if err != nil || n < 2 || connReply[1] != 0x00 {
		return false, 0
	}

	// Perform strict TLS handshake with genuine trusted CA certificates
	tlsConn := tls.Client(conn, &tls.Config{
		ServerName:         target,
		InsecureSkipVerify: false, // CRITICAL: Blocks MITM / captive portal proxies
	})
	defer tlsConn.Close()

	tlsConn.SetDeadline(time.Now().Add(timeout))
	if err := tlsConn.Handshake(); err != nil {
		return false, 0
	}

	// Verify real bidirectional HTTP data transfer
	httpReq := fmt.Sprintf("GET / HTTP/1.1\r\nHost: %s\r\nUser-Agent: Zed/0.180.0\r\nConnection: close\r\n\r\n", target)
	if _, err := tlsConn.Write([]byte(httpReq)); err != nil {
		return false, 0
	}

	respBuf := make([]byte, 256)
	nRead, err := tlsConn.Read(respBuf)
	if err != nil || nRead < 12 {
		return false, 0
	}
	if !strings.HasPrefix(string(respBuf[:nRead]), "HTTP/") {
		return false, 0
	}

	latency := time.Since(start)
	return true, latency
}
