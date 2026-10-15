package graph

import (
	"log/slog"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/walnuts1018/horse/internal/application/mailforwarding"
	"github.com/walnuts1018/horse/internal/auth"
)

type Resolver struct {
	DB                    *pgxpool.Pool
	Auth                  *auth.Service
	MailForwarding        *mailforwarding.Service
	MailForwardingEnabled bool
	Logger                *slog.Logger
	subscriptionHubMu     sync.Mutex
	subscriptionHub       *subscriptionHub
}

func (r *Resolver) getSubscriptionHub() *subscriptionHub {
	r.subscriptionHubMu.Lock()
	defer r.subscriptionHubMu.Unlock()
	if r.subscriptionHub == nil {
		logger := r.Logger
		if logger == nil {
			logger = slog.Default()
		}
		r.subscriptionHub = newSubscriptionHub(r.DB, logger)
	}
	return r.subscriptionHub
}
