// Package ephemeral provides disposable caching and atomic shared request budgets.
package ephemeral

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
	"telcopulse/services/shared/domain"
)

// Store contains bounded Redis connections. Redis never owns purchase state.
type Store struct {
	Client *redis.Client
	Prefix string
	Log    *slog.Logger
}

// Open configures short timeouts; Redis failures cannot stall service requests.
func Open(url string, log *slog.Logger) (*Store, error) {
	options, err := redis.ParseURL(url)
	if err != nil {
		return nil, errors.New("invalid REDIS_URL")
	}
	options.DialTimeout = 200 * time.Millisecond
	options.ReadTimeout = 200 * time.Millisecond
	options.WriteTimeout = 200 * time.Millisecond
	options.PoolTimeout = 200 * time.Millisecond
	options.ContextTimeoutEnabled = true
	options.MaxRetries = -1
	options.PoolSize = 8
	options.MinIdleConns = 0
	return &Store{Client: redis.NewClient(options), Prefix: "telcopulse:", Log: log}, nil
}

// Packages caches only the display catalog. Purchase validation bypasses this cache.
func (s *Store) Packages(ctx context.Context, load func(context.Context) ([]domain.Package, error)) ([]domain.Package, error) {
	cacheCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	payload, err := s.Client.Get(cacheCtx, s.Prefix+"catalog:v1").Bytes()
	cancel()
	var result []domain.Package
	if err == nil && json.Unmarshal(payload, &result) == nil && validCatalog(result) {
		return result, nil
	}
	if err != nil && !errors.Is(err, redis.Nil) {
		s.Log.Warn("catalog cache unavailable; loading authoritative catalog")
	}
	result, err = load(ctx)
	if err != nil {
		return nil, err
	}
	if !validCatalog(result) {
		return nil, errors.New("invalid authoritative catalog")
	}
	payload, err = json.Marshal(result)
	if err != nil {
		return nil, err
	}
	cacheCtx, cancel = context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	if err = s.Client.Set(cacheCtx, s.Prefix+"catalog:v1", payload, 30*time.Second).Err(); err != nil {
		s.Log.Warn("catalog cache write unavailable")
	}
	return result, nil
}
func validCatalog(items []domain.Package) bool {
	if items == nil {
		return false
	}
	for _, p := range items {
		if p.ID == "" || p.Name == "" || p.Price <= 0 || p.Days <= 0 || p.DataGB <= 0 {
			return false
		}
	}
	return true
}

var budget = redis.NewScript(`
local current=tonumber(redis.call('GET',KEYS[1]) or '0')
local ttl=redis.call('PTTL',KEYS[1])
if current >= tonumber(ARGV[1]) and ttl > 0 then return ttl end
if ttl <= 0 then
 redis.call('SET',KEYS[1],1,'PX',ARGV[2])
else
 redis.call('INCR',KEYS[1])
end
return 0
`)

// Allow atomically consumes a fixed-window budget shared by all gateway replicas.
// Positive duration means throttled. An error means no admission decision is safe.
func (s *Store) Allow(ctx context.Context, key string, limit int, window time.Duration) (time.Duration, error) {
	if limit < 1 || window < time.Millisecond {
		return 0, errors.New("invalid rate limit")
	}
	bounded, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	ms, err := budget.Run(bounded, s.Client, []string{s.Prefix + "budget:" + key}, limit, window.Milliseconds()).Int64()
	return time.Duration(ms) * time.Millisecond, err
}
