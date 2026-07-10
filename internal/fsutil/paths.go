package fsutil

import (
	"os"
	"path/filepath"
	"strings"
)

// ResolvePath expands ~ to $HOME and resolves . and .. components.
func ResolvePath(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			p = filepath.Join(home, p[2:])
		}
	} else if p == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			p = home
		}
	}

	if !filepath.IsAbs(p) {
		if cwd, err := os.Getwd(); err == nil {
			p = filepath.Join(cwd, p)
		}
	}

	return filepath.Clean(p)
}

// ClaudePath returns the Claude Code config directory (~/.claude).
func ClaudePath() string {
	return ResolvePath("~/.claude")
}

// OpenCodePath returns the OpenCode config directory (~/.config/opencode).
func OpenCodePath() string {
	return ResolvePath("~/.config/opencode")
}
