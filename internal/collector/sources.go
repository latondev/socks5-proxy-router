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

// -----------------------------------------------------------------------------
// CharlesPikachuProvider: https://raw.githubusercontent.com/CharlesPikachu/freeproxy/master/proxies.json
// -----------------------------------------------------------------------------

type CharlesPikachuProvider struct {
	endpoints []string
}

func NewCharlesPikachuProvider() *CharlesPikachuProvider {
	return &CharlesPikachuProvider{
		endpoints: []string{
			"https://raw.githubusercontent.com/CharlesPikachu/freeproxy/master/proxies.json",
			"https://charlespikachu.github.io/freeproxy/proxies.json",
		},
	}
}

func (p *CharlesPikachuProvider) Name() string {
	return "charlespikachu-freeproxy"
}

type charlesPikachuItem struct {
	IP        string `json:"ip"`
	Port      int    `json:"port"`
	Protocol  string `json:"protocol"`
	Country   string `json:"country"`
	Anonymity string `json:"anonymity"`
	Speed     int    `json:"speed"`
}

type charlesPikachuResp struct {
	UpdatedAt string               `json:"updated_at"`
	Count     int                  `json:"count"`
	Data      []charlesPikachuItem `json:"data"`
}

func (p *CharlesPikachuProvider) Fetch(ctx context.Context, client *http.Client) ([]model.Proxy, error) {
	var lastErr error
	for _, ep := range p.endpoints {
		req, err := http.NewRequestWithContext(ctx, "GET", ep, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			lastErr = fmt.Errorf("unexpected status from %s: %d", ep, resp.StatusCode)
			continue
		}

		var payload charlesPikachuResp
		err = json.NewDecoder(resp.Body).Decode(&payload)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}

		var proxies []model.Proxy
		seen := make(map[string]bool)
		for _, item := range payload.Data {
			proto := strings.ToLower(item.Protocol)
			if !strings.Contains(proto, "socks5") {
				continue
			}
			if item.Port <= 0 || item.Port > 65535 || item.IP == "" {
				continue
			}
			addr := fmt.Sprintf("%s:%d", item.IP, item.Port)
			if seen[addr] {
				continue
			}
			seen[addr] = true
			proxies = append(proxies, model.Proxy{
				IP:          item.IP,
				Port:        strconv.Itoa(item.Port),
				Country:     item.Country,
				CountryCode: item.Country,
			})
		}
		if len(proxies) > 0 {
			return proxies, nil
		}
	}
	return nil, fmt.Errorf("charlespikachu provider failed: %w", lastErr)
}
