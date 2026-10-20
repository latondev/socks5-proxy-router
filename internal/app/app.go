package app

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"socks5-pool/internal/checker"
	"socks5-pool/internal/collector"
	"socks5-pool/internal/config"
	"socks5-pool/internal/dashboard"
	"socks5-pool/internal/router"
)

// App manages the lifecycle of proxy routing, health checking, and dashboards.
type App struct {
	cfg         *config.Config
	pool        *router.ProxyPool
	logStore    *router.LogStore
	collector   *collector.Manager
	refreshChan chan struct{}

	scrapeMu       sync.RWMutex
	lastScrapeTime time.Time
	nextScrapeTime time.Time
}

func New(cfg *config.Config) *App {
	logStore := router.NewLogStore(20)
	pool := router.NewProxyPool(logStore)
	mgr := collector.NewManager(nil)
	mgr.RegisterDefaultProviders()

	return &App{
		cfg:         cfg,
		pool:        pool,
		logStore:    logStore,
		collector:   mgr,
		refreshChan: make(chan struct{}, 1),
	}
}

func (a *App) getScrapeTimes() (last, next time.Time) {
	a.scrapeMu.RLock()
	defer a.scrapeMu.RUnlock()
	return a.lastScrapeTime, a.nextScrapeTime
}

func (a *App) TriggerRefresh() {
	select {
	case a.refreshChan <- struct{}{}:
	default:
	}
}

func (a *App) refreshPool(ctx context.Context) {
	proxies, err := a.collector.Collect(ctx, a.cfg.ScrapeURL)
	if err != nil {
		log.Printf("[error] scrape failed: %v", err)
		return
	}

	alive := checker.CheckProxies(proxies, a.cfg.CheckTimeout, a.cfg.MaxLatency, a.cfg.MaxConcurrent)
	a.pool.Update(alive)

	a.scrapeMu.Lock()
	a.lastScrapeTime = time.Now()
	a.nextScrapeTime = a.lastScrapeTime.Add(a.cfg.ScrapeInterval)
	a.scrapeMu.Unlock()

	log.Printf("[main] pool refreshed: %d alive proxies", a.pool.Size())
}

// Run executes the application.
func (a *App) Run() error {
	log.Printf("socks5-pool starting...")
	log.Printf("  listen:   %s", a.cfg.ListenAddr)
	log.Printf("  status:   %s", a.cfg.StatusAddr)
	log.Printf("  source:   %s", a.cfg.ScrapeURL)
	log.Printf("  scrape:   every %s", a.cfg.ScrapeInterval)
	log.Printf("  max-lat:  %s", a.cfg.MaxLatency)
	if a.cfg.AuthUser != "" && a.cfg.AuthPass != "" {
		log.Printf("  auth:     ENABLED (user: %s)", a.cfg.AuthUser)
	} else {
		log.Printf("  auth:     DISABLED")
	}

	ctx := context.Background()

	// 1. Status dashboard (starts immediately so /api/feed and UI are up instantly)
	go func() {
		dash := dashboard.NewServer(
			a.pool,
			a.cfg.ListenAddr,
			a.cfg.AuthUser,
			a.cfg.AuthPass,
			a.logStore,
			a.TriggerRefresh,
			a.getScrapeTimes,
		)
		log.Printf("[status] dashboard at http://%s", a.cfg.StatusAddr)
		if err := dash.Start(a.cfg.StatusAddr); err != nil {
			log.Printf("[status] failed to start: %v", err)
		}
	}()

	// 2. Initial scrape + background periodic refresh
	go func() {
		a.refreshPool(ctx)
		if a.pool.Size() == 0 {
			log.Printf("[warn] no alive proxies found yet, waiting for harvester feed or next cycle")
		}

		ticker := time.NewTicker(a.cfg.ScrapeInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				a.refreshPool(ctx)
			case <-a.refreshChan:
				log.Printf("[main] refresh triggered")
				a.refreshPool(ctx)
				ticker.Reset(a.cfg.ScrapeInterval)
			}
		}
	}()

	// 3. Proactive Active Watchdog (runs every 25 seconds)
	go func() {
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			current, ok := a.pool.Current()
			if !ok {
				continue
			}
			okCheck, lat := checker.CheckHTTPS(current, 3*time.Second)
			if !okCheck {
				log.Printf("[watchdog] active proxy %s failed proactive TLS test, pruning immediately...", current.Addr())
				a.pool.RemoveFailed(current.Addr())
				a.logStore.Add(fmt.Sprintf("Watchdog pruned dead proxy %s (%s)", current.Addr(), current.Country), "watchdog")
			} else {
				current.Latency = lat
			}
		}
	}()

	// 4. Keep pool healthy and populate when low
	go func() {
		for {
			time.Sleep(20 * time.Second)
			if a.pool.Size() < 5 {
				log.Printf("[main] pool low (%d remaining), triggering fresh scrape", a.pool.Size())
				a.TriggerRefresh()
			}
		}
	}()

	// Start SOCKS5 Router server (blocks)
	server := router.NewServer(
		a.cfg.ListenAddr,
		a.pool,
		a.cfg.AuthUser,
		a.cfg.AuthPass,
		a.logStore,
		a.TriggerRefresh,
	)
	return server.Start()
}

// Run parses config and runs the application.
func Run() error {
	cfg := config.Parse()
	app := New(cfg)
	return app.Run()
}
