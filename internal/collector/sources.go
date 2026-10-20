package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"socks5-pool/internal/model"
)

var proxyRegex = regexp.MustCompile(`(?:socks5://)?(\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b):(\d{1,5})`)

// ParseTextProxies parses raw IP:Port or socks5://IP:Port from plain text.
func ParseTextProxies(text string) []model.Proxy {
	matches := proxyRegex.FindAllStringSubmatch(text, -1)
	seen := make(map[string]bool)
	var proxies []model.Proxy

	for _, m := range matches {
		ip := strings.TrimSpace(m[1])
		port := strings.TrimSpace(m[2])
		portNum, err := strconv.Atoi(port)
		if err != nil || portNum <= 0 || portNum > 65535 {
			continue
		}

		addr := ip + ":" + port
		if seen[addr] {
			continue
		}
		seen[addr] = true
		proxies = append(proxies, model.Proxy{
			IP:   ip,
			Port: port,
		})
	}
	return proxies
}

// -----------------------------------------------------------------------------
// ProxifyProvider: https://api.proxify.vn/api/proxy-free
// -----------------------------------------------------------------------------

type ProxifyProvider struct {
	endpoint string
}

func NewProxifyProvider() *ProxifyProvider {
	return &ProxifyProvider{
		endpoint: "https://api.proxify.vn/api/proxy-free",
	}
}

func (p *ProxifyProvider) Name() string {
	return "proxify.vn"
}

type proxifyResp struct {
	Data struct {
		Proxies []struct {
			IP       string  `json:"ip"`
			Port     int     `json:"port"`
			Protocol string  `json:"protocol"`
			Alive    bool    `json:"alive"`
			SSL      bool    `json:"ssl"`
			Timeout  float64 `json:"timeout"`
			IPData   struct {
				Country     string `json:"country"`
				CountryCode string `json:"countryCode"`
				City        string `json:"city"`
			} `json:"ip_data"`
		} `json:"proxies"`
	} `json:"data"`
}

func (p *ProxifyProvider) Fetch(ctx context.Context, client *http.Client) ([]model.Proxy, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", p.endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Referer", "https://proxify.vn/")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var data proxifyResp
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode error: %w", err)
	}

	type candidate struct {
		proxy   model.Proxy
		timeout float64
	}
	var candidates []candidate

	for _, item := range data.Data.Proxies {
		if strings.ToLower(item.Protocol) != "socks5" || !item.Alive || !item.SSL {
			continue
		}
		candidates = append(candidates, candidate{
			proxy: model.Proxy{
				IP:          strings.TrimSpace(item.IP),
				Port:        strconv.Itoa(item.Port),
				Country:     strings.TrimSpace(item.IPData.Country),
				CountryCode: strings.TrimSpace(item.IPData.CountryCode),
				City:        strings.TrimSpace(item.IPData.City),
			},
			timeout: item.Timeout,
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].timeout < candidates[j].timeout
	})

	result := make([]model.Proxy, len(candidates))
	for i, c := range candidates {
		result[i] = c.proxy
	}
	return result, nil
}

// -----------------------------------------------------------------------------
// TextURLProvider: For plain text feeds like Monosans and socks5-proxy
// -----------------------------------------------------------------------------

type TextURLProvider struct {
	name string
	url  string
}

func NewTextURLProvider(name, url string) *TextURLProvider {
	return &TextURLProvider{name: name, url: url}
}

func (p *TextURLProvider) Name() string {
	return p.name
}

func (p *TextURLProvider) Fetch(ctx context.Context, client *http.Client) ([]model.Proxy, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", p.url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body failed: %w", err)
	}

	return ParseTextProxies(string(body)), nil
}
