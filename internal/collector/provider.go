package collector

import (
	"context"
	"net/http"

	"socks5-pool/internal/model"
)

// Provider defines the interface for any proxy source/feed.
type Provider interface {
	// Name returns a friendly identifier for the provider (e.g. "proxify", "monosans").
	Name() string
	// Fetch gathers raw candidate proxies from the upstream source.
	Fetch(ctx context.Context, client *http.Client) ([]model.Proxy, error)
}
