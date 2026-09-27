package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var proxyRegex = regexp.MustCompile(`(?:socks5://)?(\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b):(\d{1,5})`)

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

// Scrape fetches proxies from one or multiple sources.
func Scrape(source string) ([]Proxy, error) {
	t := &http.Transport{}
	t.RegisterProtocol("file", http.NewFileTransport(http.Dir("/")))
	client := &http.Client{
		Transport: t,
		Timeout:   15 * time.Second,
	}

	// If user provided a specific file or custom URL
	if source != "" && source != "auto" && source != "default" {
		return scrapeSingleSource(client, source)
	}

	// Default: Multi-source scraping
	log.Printf("[scraper] starting multi-source scraping (Proxify.vn + Monosans + socks5-proxy)...")
	var all []Proxy
	seen := make(map[string]bool)

	// 1. Proxify.vn (high quality verified socks5 with geo & timeout)
	proxifyList := scrapeProxify(client)
	for _, p := range proxifyList {
		if !seen[p.Addr()] {
			seen[p.Addr()] = true
			all = append(all, p)
		}
	}
	log.Printf("[scraper] loaded %d proxies from Proxify.vn", len(proxifyList))

	// 2. Monosans socks5 list
	monosansList, err := scrapeSingleSource(client, "https://raw.githubusercontent.com/monosans/proxy-list/main/proxies/socks5.txt")
	if err == nil {
		added := 0
		for _, p := range monosansList {
			if !seen[p.Addr()] {
				seen[p.Addr()] = true
				all = append(all, p)
				added++
			}
		}
		log.Printf("[scraper] added %d proxies from Monosans list", added)
	}

	// 3. Fallback: socks5-proxy.github.io
	defaultList, err := scrapeSingleSource(client, "https://socks5-proxy.github.io/")
	if err == nil {
		added := 0
		for _, p := range defaultList {
			if !seen[p.Addr()] {
				seen[p.Addr()] = true
				all = append(all, p)
				added++
			}
		}
		log.Printf("[scraper] added %d proxies from socks5-proxy.github.io", added)
	}

	log.Printf("[scraper] total candidate proxies collected: %d", len(all))
	return all, nil
}

func scrapeProxify(client *http.Client) []Proxy {
	req, err := http.NewRequest("GET", "https://api.proxify.vn/api/proxy-free", nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Referer", "https://proxify.vn/")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[scraper] proxify.vn fetch error: %v", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var data proxifyResp
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil
	}

	type candidate struct {
		proxy   Proxy
		timeout float64
	}
	var candidates []candidate

	for _, item := range data.Data.Proxies {
		if strings.ToLower(item.Protocol) != "socks5" || !item.Alive || !item.SSL {
			continue
		}
		candidates = append(candidates, candidate{
			proxy: Proxy{
				IP:          strings.TrimSpace(item.IP),
				Port:        strconv.Itoa(item.Port),
				Country:     strings.TrimSpace(item.IPData.Country),
				CountryCode: strings.TrimSpace(item.IPData.CountryCode),
				City:        strings.TrimSpace(item.IPData.City),
			},
			timeout: item.Timeout,
		})
	}

	// Sort candidates by Proxify's reported timeout ascending so fastest are checked first
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].timeout < candidates[j].timeout
	})

	result := make([]Proxy, len(candidates))
	for i, c := range candidates {
		result[i] = c.proxy
	}
	return result
}

func scrapeSingleSource(client *http.Client, url string) ([]Proxy, error) {
	req, err := http.NewRequest("GET", url, nil)
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

	matches := proxyRegex.FindAllStringSubmatch(string(body), -1)
	seen := make(map[string]bool)
	var proxies []Proxy

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
		proxies = append(proxies, Proxy{
			IP:   ip,
			Port: port,
		})
	}

	return proxies, nil
}
