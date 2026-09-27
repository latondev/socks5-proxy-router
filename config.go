package main

import (
	"flag"
	"os"
	"time"
)

type Config struct {
	ListenAddr     string
	StatusAddr     string
	ScrapeURL      string
	ScrapeInterval time.Duration
	CheckTimeout   time.Duration
	MaxLatency     time.Duration
	MaxConcurrent  int
}

func ParseConfig() *Config {
	cfg := &Config{}
	flag.StringVar(&cfg.ListenAddr, "listen", "127.0.0.1:1080", "local SOCKS5 listen address")
	flag.StringVar(&cfg.StatusAddr, "status", "127.0.0.1:8080", "HTTP status dashboard address")
	flag.StringVar(&cfg.ScrapeURL, "url", "auto", "proxy list URL, file path, or 'auto' for multi-source")
	flag.DurationVar(&cfg.ScrapeInterval, "scrape-interval", 20*time.Minute, "scrape interval")
	flag.DurationVar(&cfg.CheckTimeout, "check-timeout", 4*time.Second, "proxy check timeout")
	flag.DurationVar(&cfg.MaxLatency, "max-latency", 2500*time.Millisecond, "max allowed latency (e.g. 1500ms, 2s)")
	flag.IntVar(&cfg.MaxConcurrent, "max-concurrent", 40, "max concurrent health checks")
	flag.Parse()

	// Cloud deployment: always use fixed ports
	// SOCKS5 on 1080, status on 8080
	if os.Getenv("PORT") != "" {
		cfg.ListenAddr = "0.0.0.0:1080"
		cfg.StatusAddr = "0.0.0.0:8080"
	}

	return cfg
}
