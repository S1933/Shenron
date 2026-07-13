package adapter

// Optional adapter capabilities. The sync runtime probes for these with type
// assertions, so an adapter opts in simply by implementing the methods.

// MergingAdapter merges accumulated fragments into an existing shared config
// file (e.g. opencode.json). Standalone-file adapters (Claude, Codex) do not
// implement it.
type MergingAdapter interface {
	// MergeFile upserts fragments into existing and returns the new file bytes,
	// or nil when path is not the shared config file the adapter owns.
	MergeFile(path string, existing []byte, fragments map[string]any) ([]byte, error)
	// ConfigPath returns the absolute path of the shared config file.
	ConfigPath() string
}

// ManagedPruner removes leaves that shenron previously managed (recorded in
// state.Managed) but that the current pivot no longer generates, before
// upserting the current fragments. It preserves entries shenron never owned.
type ManagedPruner interface {
	PruneManaged(path string, existing []byte, managed map[string][]string, fragments map[string]any) ([]byte, error)
}

// PivotDirectoryAware receives the directory of the pivot file, used to resolve
// relative promptFile references during generation.
type PivotDirectoryAware interface {
	SetPivotDir(string)
}
