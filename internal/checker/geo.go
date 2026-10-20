package checker

import (
	"fmt"
	"net"
	"strings"
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

// IsCountryBlocked checks whether country name or code is in the restricted list.
func IsCountryBlocked(country, code string) bool {
	if country != "" && blockedCountries[strings.ToLower(strings.TrimSpace(country))] {
		return true
	}
	if code != "" && blockedCountries[strings.ToLower(strings.TrimSpace(code))] {
		return true
	}
	return false
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
