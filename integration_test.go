package luatable

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func readTestdata(t *testing.T, name string) string {
	t.Helper()

	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("cannot read testdata/%s: %s", name, err)
	}
	return string(b)
}

// lookup walks a path of generic map keys and returns the value found there.
func lookup(t *testing.T, value any, path ...string) any {
	t.Helper()

	current := value
	for _, key := range path {
		m, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("cannot descend into %T using key %q", current, key)
		}
		next, ok := m[key]
		if !ok {
			t.Fatalf("key %q not found in %v", key, m)
		}
		current = next
	}
	return current
}

func TestParseTestdataConfig(t *testing.T) {
	value, err := Parse(readTestdata(t, "config.lua"))
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	root, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("the configuration table should decode to a map; got %T", value)
	}
	if len(root) != 12 {
		t.Fatalf("unexpected number of entries: %d; want 12 (%v)", len(root), root)
	}

	cases := []struct {
		key  string
		want any
	}{
		{"name", "luatable"},
		{"version", "1.0.0"},
		{"debug", false},
		{"timeout", int64(30)},
		{"ratio", 0.75},
		{"max-connections", int64(128)},
		{"42", "answer"},
		{"true", "yes"},
		{"features", []any{"parse", "nested", "comments"}},
	}
	for _, tc := range cases {
		if got := root[tc.key]; !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("key %q: got %#v; want %#v", tc.key, got, tc.want)
		}
	}

	servers, ok := root["servers"].([]any)
	if !ok {
		t.Fatalf("servers should be an array; got %T", root["servers"])
	}
	if len(servers) != 2 {
		t.Fatalf("unexpected number of servers: %d", len(servers))
	}
	if got := lookup(t, servers[0], "host"); got != "alpha" {
		t.Fatalf("unexpected first server host: %#v", got)
	}
	if got := lookup(t, servers[0], "port"); got != int64(8080) {
		t.Fatalf("unexpected first server port: %#v", got)
	}
	if got := lookup(t, servers[1], "host"); got != "beta" {
		t.Fatalf("unexpected second server host: %#v", got)
	}

	if got := lookup(t, root, "nested", "level1", "level2", "level3"); got != "deep" {
		t.Fatalf("unexpected deeply nested value: %#v", got)
	}

	long, ok := root["long"].(string)
	if !ok {
		t.Fatalf("long should be a string; got %T", root["long"])
	}
	if !strings.Contains(long, "multi-line\nlong string") {
		t.Fatalf("unexpected long string value: %q", long)
	}
}

func TestParseTestdataModule(t *testing.T) {
	table, err := ParseModule(readTestdata(t, "module.lua"))
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	cases := []struct {
		key  string
		want any
	}{
		{"answer", int64(42)},
		{"greeting", `hello "world"`},
		{"hex", int64(255)},
		{"fraction", 125.0},
		{"list", []any{int64(1), int64(2), int64(3), nil, int64(5)}},
	}
	for _, tc := range cases {
		got, ok := table.Get(tc.key)
		if !ok {
			t.Fatalf("key %q not found", tc.key)
		}
		if !reflect.DeepEqual(ToInterface(got), tc.want) {
			t.Fatalf("key %q: got %#v; want %#v", tc.key, got, tc.want)
		}
	}
}

func TestParseTestdataComments(t *testing.T) {
	value, err := Parse(readTestdata(t, "comments.lua"))
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	want := map[string]any{
		"a": int64(1),
		"b": int64(2),
		"c": []any{int64(1), int64(2)},
	}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("unexpected value; got %#v; want %#v", value, want)
	}
}
