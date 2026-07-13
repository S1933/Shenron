package opencode

import (
	"encoding/json"
	"testing"
	"unicode/utf8"
)

// FuzzMergeFile exercises the OpenCode config merge on arbitrary existing
// documents and fragment ids. It asserts the core invariants the sync runtime
// relies on:
//
//   - MergeFile never panics on any input.
//   - When it succeeds, the output is always valid JSON.
//   - Every top-level key present in a well-formed existing object survives the
//     merge (the merge only upserts, so native/foreign keys are never dropped).
//   - The merge is idempotent: merging its own output with the same fragments
//     reproduces it byte-for-byte.
func FuzzMergeFile(f *testing.F) {
	seeds := []string{
		"",
		"{}",
		`{"theme":"dark"}`,
		`{"agent":{"native":{"mode":"subagent"}},"provider":{"default":"anthropic"}}`,
		`{"command":{"ship":{"template":"go"}},"$schema":"https://opencode"}`,
		`{"agent":{"build":{"mode":"primary"}},"command":{}}`,
		`{"dup":1,"dup":2,"agent":{}}`,
		`{"nested":{"a":{"b":[1,2,{"c":true}]}}}`,
		`{"unicode":"café é 😀","agent":{"x":{}}}`,
		`{} trailing garbage`,
		`not json`,
		`[1,2,3]`,
	}
	for _, s := range seeds {
		f.Add([]byte(s), "build", "ship", "a subagent")
	}

	adapter := NewAdapterWithBaseDir("", "")
	const path = "opencode.json"

	f.Fuzz(func(t *testing.T, existing []byte, agentID, cmdID, body string) {
		// Pivot ids always come from YAML, so they are valid UTF-8. Non-UTF-8
		// leaf ids are out of contract: json.Marshal rewrites their bytes to
		// U+FFFD, which the raw in-memory key can no longer match on a second
		// pass, breaking idempotence. Skip them rather than assert an invariant
		// the real pipeline never has to uphold.
		if !utf8.ValidString(agentID) || !utf8.ValidString(cmdID) {
			return
		}

		fragments := map[string]any{
			"agent." + agentID: map[string]any{"mode": "subagent", "description": body},
			"command." + cmdID: map[string]any{"template": body},
		}

		out, err := adapter.MergeFile(path, existing, fragments)
		if err != nil {
			// Malformed existing input legitimately errors; nothing else to check.
			return
		}
		if out == nil {
			t.Fatalf("MergeFile returned nil output without error for path %q", path)
		}

		if !json.Valid(out) {
			t.Fatalf("MergeFile produced invalid JSON:\n%s", out)
		}

		// Foreign-key preservation: only checkable when the existing document is a
		// standard JSON object (parseOrderedObject is more lenient about trailing
		// data than encoding/json, so guard on a strict re-parse).
		var existingObj map[string]json.RawMessage
		if json.Unmarshal(existing, &existingObj) == nil {
			var outObj map[string]json.RawMessage
			if err := json.Unmarshal(out, &outObj); err != nil {
				t.Fatalf("output not an object: %v\n%s", err, out)
			}
			for key := range existingObj {
				if _, ok := outObj[key]; !ok {
					t.Errorf("top-level key %q dropped by merge\nexisting: %s\nout: %s", key, existing, out)
				}
			}
		}

		// Idempotence: re-merging the output with the same fragments is a no-op.
		out2, err := adapter.MergeFile(path, out, fragments)
		if err != nil {
			t.Fatalf("second merge failed on valid output: %v\n%s", err, out)
		}
		if string(out2) != string(out) {
			t.Errorf("merge not idempotent:\nfirst:\n%s\nsecond:\n%s", out, out2)
		}
	})
}
