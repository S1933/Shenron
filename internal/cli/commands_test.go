package cli_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/S1933/Shenron/internal/cli"
)

// TestRootSubcommandNames asserts the root command advertises exactly the
// seven top-level subcommands and no "package" parent.
func TestRootSubcommandNames(t *testing.T) {
	root := cli.NewRootCmd()

	want := []string{"diff", "doctor", "explain", "install", "list", "push", "update"}
	var got []string
	for _, c := range root.Commands() {
		got = append(got, c.Name())
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("root subcommands = %v, want %v", got, want)
	}
	for _, c := range root.Commands() {
		if c.Name() == "package" {
			t.Fatalf("root has a 'package' subcommand; the parent was supposed to be removed")
		}
	}
}

// TestRootHasStorePersistentFlag asserts --store is a persistent root flag
// and that every subcommand inherits it.
func TestRootHasStorePersistentFlag(t *testing.T) {
	root := cli.NewRootCmd()

	flag := root.PersistentFlags().Lookup("store")
	if flag == nil {
		t.Fatal("root has no --store persistent flag")
	}
	if flag.DefValue != "" {
		t.Errorf("--store default = %q, want empty (resolution lives in storeAt)", flag.DefValue)
	}

	for _, c := range root.Commands() {
		if c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		if c.InheritedFlags().Lookup("store") == nil {
			t.Errorf("subcommand %q does not inherit --store", c.Name())
		}
	}
}

// TestStoreFlagPropagatesToStoreRoot installs a fixture package with
// --store pointing at a custom directory and asserts the install side
// effects (index file, snapshot directory) live under that directory.
func TestStoreFlagPropagatesToStoreRoot(t *testing.T) {
	custom := t.TempDir()
	source := writeCLIInstallFixture(t)

	root := cli.NewRootCmd()
	root.SetArgs([]string{"--store", custom, "install", source})
	if err := root.Execute(); err != nil {
		t.Fatalf("install --store %s: %v", custom, err)
	}

	// The store writes its index at <root>/index.json (or similar). If
	// --store did not propagate, the index would live under $HOME/.shenron
	// instead. We assert at least one file exists under custom.
	entries, err := os.ReadDir(custom)
	if err != nil {
		t.Fatalf("read %s: %v", custom, err)
	}
	if len(entries) == 0 {
		t.Fatalf("store root %s is empty; --store did not propagate", custom)
	}
}

// TestSubcommandArgsRejectZeroAndMany drives each subcommand with bad
// arity and asserts the result is a validation failure (arg count or
// unknown command) rather than a successful run. This guards against
// accidentally loosening the ExactArgs constraint.
func TestSubcommandArgsRejectZeroAndMany(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"install", []string{}},
		{"install", []string{"a", "b"}},
		{"update", []string{}},
		{"update", []string{"a", "b"}},
		{"diff", []string{}},
		{"diff", []string{"a", "b"}},
		{"push", []string{}},
		{"push", []string{"a", "b"}},
		{"list", []string{"x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/"+strings.Join(tc.args, "_"), func(t *testing.T) {
			root := cli.NewRootCmd()
			args := append([]string{tc.name}, tc.args...)
			// Point HOME at an empty dir so list never touches real packages.
			t.Setenv("HOME", t.TempDir())
			root.SetArgs(args)
			err := root.Execute()
			if err == nil {
				t.Fatalf("expected validation failure for args=%v", tc.args)
			}
			msg := err.Error()
			// For ExactArgs(1) commands: cobra reports an "arg" error.
			// For NoArgs list: cobra reports an "unknown command" error
			// when a positional is supplied. Both are validation failures.
			if !strings.Contains(msg, "arg") && !strings.Contains(msg, "unknown command") {
				t.Errorf("error %q is not a validation failure", msg)
			}
		})
	}
}

// TestSubcommandFlags asserts each subcommand exposes the documented flags.
func TestSubcommandFlags(t *testing.T) {
	root := cli.NewRootCmd()
	must := func(name, flag string) {
		t.Helper()
		for _, c := range root.Commands() {
			if c.Name() == name {
				if c.Flags().Lookup(flag) == nil {
					t.Errorf("subcommand %q missing --%s flag", name, flag)
				}
				return
			}
		}
		t.Errorf("subcommand %q not found on root", name)
	}
	must("install", "ref")
	must("update", "source")
	must("update", "ref")
	must("diff", "target")
	must("push", "target")
	must("push", "force")
	must("push", "allow-permissions")
}

// TestSilenceUsageOnSubcommands asserts each subcommand suppresses the
// usage banner on errors. Cobra's SilenceUsage is a per-command field;
// engine errors should not print the full usage, only the error message.
func TestSilenceUsageOnSubcommands(t *testing.T) {
	root := cli.NewRootCmd()
	for _, c := range root.Commands() {
		if c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		if !c.SilenceUsage {
			t.Errorf("subcommand %q does not set SilenceUsage", c.Name())
		}
	}
}

func writeCLIInstallFixture(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "package")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "shenron-package.yaml"), []byte(`schemaVersion: "1"
name: surface-fixture
version: 0.1.0
description: Surface test fixture.
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "shenron.yaml"), []byte(`version: "1"
agents: []
`), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}
