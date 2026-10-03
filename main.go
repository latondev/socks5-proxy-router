package main

import (
	"fmt"
	"log"
	"sync"
	"time"
)

var (
	lastScrapeTime time.Time
	nextScrapeTime time.Time
	scrapeMu       sync.RWMutex
	refreshChan    = make(chan struct{}, 1)
)

func getScrapeTimes() (last, next time.Time) {
	scrapeMu.RLock()
	defer scrapeMu.RUnlock()
	return lastScrapeTime, nextScrapeTime
}

func main() {
	cfg := ParseConfig()

	log.Printf("socks5-pool starting...")
	log.Printf("  listen:   %s", cfg.ListenAddr)
	log.Printf("  status:   %s", cfg.StatusAddr)
	log.Printf("  source:   %s", cfg.ScrapeURL)
	log.Printf("  scrape:   every %s", cfg.ScrapeInterval)
	log.Printf("  max-lat:  %s", cfg.MaxLatency)
	if cfg.AuthUser != "" && cfg.AuthPass != "" {
		log.Printf("  auth:     ENABLED (user: %s)", cfg.AuthUser)
	} else {
		log.Printf("  auth:     DISABLED")
	}

	pool := NewProxyPool()

	// Initial scrape + check
	refreshPool(cfg, pool)

	if pool.Size() == 0 {
		log.Printf("[warn] no alive proxies found, will retry on next scrape cycle")
	}

	// Background: periodic scrape + manual refresh
	go func() {
		ticker := time.NewTicker(cfg.ScrapeInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				refreshPool(cfg, pool)
			case <-refreshChan:
				log.Printf("[main] manual refresh triggered")
				refreshPool(cfg, pool)
				ticker.Reset(cfg.ScrapeInterval)
			}
		}
	}()

	// Background: Proactive Active Watchdog (runs every 25 seconds)
	// Actively tests the current active proxy with real TLS handshake.
	// If it fails or dies, it prunes it immediately before any client experiences a hang!
	go func() {
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			current, ok := pool.Current()
			if !ok {
				continue
			}
			okCheck, lat := checkHTTPS(current, 3*time.Second)
			if !okCheck {
				log.Printf("[watchdog] active proxy %s failed proactive TLS test, pruning immediately...", current.Addr())
				pool.RemoveFailed(current.Addr())
				addActivityLog(fmt.Sprintf("Watchdog pruned dead proxy %s (%s)", current.Addr(), current.Country), "watchdog")
			} else {
				current.Latency = lat
			}
		}
	}()

	// Background: keep pool healthy and populate when low
	go func() {
		for {
			time.Sleep(20 * time.Second)
			if pool.Size() < 5 {
				log.Printf("[main] pool low (%d remaining), triggering fresh scrape", pool.Size())
				TriggerRefresh()
			}
		}
	}()

	// Background: status dashboard
	go func() {
		status := NewStatusServer(pool, cfg.ListenAddr, cfg.AuthUser, cfg.AuthPass)
		log.Printf("[status] dashboard at http://%s", cfg.StatusAddr)
		if err := status.Start(cfg.StatusAddr); err != nil {
			log.Printf("[status] failed to start: %v", err)
		}
	}()

	// Start SOCKS5 server (blocks)
	server := NewServer(cfg.ListenAddr, pool, cfg.AuthUser, cfg.AuthPass)
	log.Fatal(server.Start())
}

func refreshPool(cfg *Config, pool *ProxyPool) {
	proxies, err := Scrape(cfg.ScrapeURL)
	if err != nil {
		log.Printf("[error] scrape failed: %v", err)
		return
	}

	alive := CheckProxies(proxies, cfg.CheckTimeout, cfg.MaxLatency, cfg.MaxConcurrent)
	pool.Update(alive)

	scrapeMu.Lock()
	lastScrapeTime = time.Now()
	nextScrapeTime = lastScrapeTime.Add(cfg.ScrapeInterval)
	scrapeMu.Unlock()

	log.Printf("[main] pool refreshed: %d alive proxies", pool.Size())
}

func TriggerRefresh() {
	select {
	case refreshChan <- struct{}{}:
	default:
	}
}
