package cli

import (
	"context"
	"errors"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/config"
	"github.com/revenuecat/cli/internal/output"
)

type runtimeKey struct{}

type Runtime struct {
	Globals *Globals
	Config  *config.Config
	Out     *output.Renderer

	client *api.Client
}

func WithRuntime(ctx context.Context, r *Runtime) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, runtimeKey{}, r)
}

func RuntimeFrom(ctx context.Context) *Runtime {
	r, _ := ctx.Value(runtimeKey{}).(*Runtime)
	return r
}

// API returns a lazily-initialized API client built from the active config.
func (r *Runtime) API() (*api.Client, error) {
	if r.client != nil {
		return r.client, nil
	}
	if r.Config.APIKey == "" {
		return nil, ErrNotAuthenticated
	}
	r.client = api.NewClient(api.Options{
		APIKey:  r.Config.APIKey,
		BaseURL: r.Config.BaseURL,
	})
	return r.client, nil
}

var ErrNotAuthenticated = errors.New("not authenticated: run `rc login` or pass --api-key / set RC_API_KEY")

func ExitCodeFor(err error) int {
	if err == nil {
		return 0
	}
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		switch apiErr.Type {
		case "unauthorized", "authentication_error":
			return 4
		case "resource_missing":
			return 5
		case "rate_limit_exceeded":
			return 6
		}
	}
	if errors.Is(err, ErrNotAuthenticated) {
		return 4
	}
	return 1
}
