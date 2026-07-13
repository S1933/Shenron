package adapter

import (
	"io/fs"

	"github.com/S1933/Shenron/internal/pivot"
)

// GeneratedFile is one file an adapter wants written, carrying the metadata the
// sync runtime needs to route, write, and report it.
type GeneratedFile struct {
	Path       string      // absolute destination path
	Content    []byte      // file body
	Mode       fs.FileMode // permission bits for the atomic write
	Adapter    string      // owning adapter name (e.g. "opencode")
	ResourceID string      // pivot resource id this file came from, when applicable
}

// GenerationResult is the immutable output of one Generate call. Files are the
// standalone outputs; Fragments carries nested-config contributions (e.g.
// OpenCode's agent/command entries) that a MergingAdapter later folds into a
// shared file. Standalone-file adapters leave Fragments nil.
type GenerationResult struct {
	Files     []GeneratedFile
	Fragments map[string]any
}

// Adapter translates a pivot file into native configuration for one tool.
//
// Generate is a single, side-effect-free pass over the whole pivot: adapters
// build every file (and any config fragments) from local state and return them,
// so the same adapter instance can be reused and is safe under concurrency.
// Optional behaviors — merging fragments into a shared file, pruning managed
// entries, learning the pivot directory — are expressed as capability
// interfaces in capabilities.go rather than mandatory methods.
type Adapter interface {
	Name() string
	ValidateAgent(pivot.AgentDefinition) error
	Generate(*pivot.PivotFile) (GenerationResult, error)
	TargetPaths() []string
}
