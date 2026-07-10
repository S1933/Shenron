package cli

import "fmt"

func errUnknownTarget(name string) error {
	return fmt.Errorf("unknown target %q (available: opencode)", name)
}
