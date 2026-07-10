package cli

import (
	"github.com/jnuel/agentsync/internal/adapter"
	"github.com/jnuel/agentsync/internal/adapter/opencode"
)

// Registry returns all available adapters keyed by name.
func Registry() map[string]adapter.Adapter {
	return map[string]adapter.Adapter{
		"opencode": opencode.NewAdapter(),
	}
}

// ResolveTargets returns adapters matching the target flag (empty = all).
func ResolveTargets(target string) (map[string]adapter.Adapter, error) {
	all := Registry()
	if target == "" {
		return all, nil
	}
	a, ok := all[target]
	if !ok {
		return nil, errUnknownTarget(target)
	}
	return map[string]adapter.Adapter{target: a}, nil
}
