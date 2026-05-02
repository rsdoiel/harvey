package harvey

import (
	"testing"
	"time"
)

func openTestCache(t *testing.T) *ModelCache {
	t.Helper()
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	mc, err := OpenModelCache(ws, "")
	if err != nil {
		t.Fatalf("OpenModelCache: %v", err)
	}
	t.Cleanup(func() { mc.Close() })
	return mc
}

func TestModelCacheSetGet(t *testing.T) {
	mc := openTestCache(t)

	cap := &ModelCapability{
		Name:          "llama3.2:latest",
		Family:        "llama",
		ParameterSize: "3.2B",
		Quantization:  "Q4_K_M",
		SizeBytes:     2_000_000_000,
		ContextLength: 131072,
		SupportsTools: CapYes,
		SupportsEmbed: CapNo,
		ProbeLevel:    "fast",
		ProbedAt:      time.Now().Truncate(time.Second),
	}

	if err := mc.Set(cap); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := mc.Get("llama3.2:latest")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil for known model")
	}
	if got.Name != cap.Name {
		t.Errorf("Name: got %q want %q", got.Name, cap.Name)
	}
	if got.SupportsTools != CapYes {
		t.Errorf("SupportsTools: got %v want CapYes", got.SupportsTools)
	}
	if got.SupportsEmbed != CapNo {
		t.Errorf("SupportsEmbed: got %v want CapNo", got.SupportsEmbed)
	}
	if got.ContextLength != 131072 {
		t.Errorf("ContextLength: got %d want 131072", got.ContextLength)
	}
	if got.ProbeLevel != "fast" {
		t.Errorf("ProbeLevel: got %q want %q", got.ProbeLevel, "fast")
	}
}

func TestModelCacheGetMissing(t *testing.T) {
	mc := openTestCache(t)

	got, err := mc.Get("nonexistent:model")
	if err != nil {
		t.Fatalf("Get on missing model returned error: %v", err)
	}
	if got != nil {
		t.Errorf("Get on missing model returned non-nil: %+v", got)
	}
}

func TestModelCacheUpsertOverwrites(t *testing.T) {
	mc := openTestCache(t)

	first := &ModelCapability{
		Name:          "mistral:latest",
		SupportsTools: CapUnknown,
		SupportsEmbed: CapUnknown,
		ProbeLevel:    "none",
		ProbedAt:      time.Now().Truncate(time.Second),
	}
	if err := mc.Set(first); err != nil {
		t.Fatal(err)
	}

	second := &ModelCapability{
		Name:          "mistral:latest",
		Family:        "mistral",
		ParameterSize: "7.2B",
		SupportsTools: CapYes,
		SupportsEmbed: CapNo,
		ProbeLevel:    "thorough",
		ProbedAt:      time.Now().Truncate(time.Second),
	}
	if err := mc.Set(second); err != nil {
		t.Fatal(err)
	}

	got, err := mc.Get("mistral:latest")
	if err != nil {
		t.Fatal(err)
	}
	if got.SupportsTools != CapYes {
		t.Errorf("SupportsTools after upsert: got %v want CapYes", got.SupportsTools)
	}
	if got.ProbeLevel != "thorough" {
		t.Errorf("ProbeLevel after upsert: got %q want %q", got.ProbeLevel, "thorough")
	}
	if got.Family != "mistral" {
		t.Errorf("Family after upsert: got %q want %q", got.Family, "mistral")
	}
}

func TestModelCacheDelete(t *testing.T) {
	mc := openTestCache(t)

	cap := &ModelCapability{
		Name:       "smollm2:1.7b",
		ProbeLevel: "fast",
		ProbedAt:   time.Now().Truncate(time.Second),
	}
	if err := mc.Set(cap); err != nil {
		t.Fatal(err)
	}
	if err := mc.Delete("smollm2:1.7b"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, err := mc.Get("smollm2:1.7b")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Error("expected nil after Delete, got entry")
	}
}

func TestModelCacheDeleteNoOp(t *testing.T) {
	mc := openTestCache(t)

	if err := mc.Delete("ghost:model"); err != nil {
		t.Errorf("Delete of missing model returned error: %v", err)
	}
}

func TestModelCacheAll(t *testing.T) {
	mc := openTestCache(t)

	names := []string{"alpha:latest", "beta:latest", "gamma:latest"}
	for _, n := range names {
		if err := mc.Set(&ModelCapability{Name: n, ProbeLevel: "none", ProbedAt: time.Now()}); err != nil {
			t.Fatalf("Set %s: %v", n, err)
		}
	}

	all, err := mc.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != len(names) {
		t.Fatalf("All returned %d entries, want %d", len(all), len(names))
	}
	for i, c := range all {
		if c.Name != names[i] {
			t.Errorf("all[%d].Name = %q, want %q", i, c.Name, names[i])
		}
	}
}

func TestModelCacheAllEmpty(t *testing.T) {
	mc := openTestCache(t)

	all, err := mc.All()
	if err != nil {
		t.Fatalf("All on empty cache: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("expected empty slice, got %d entries", len(all))
	}
}

func TestCapabilityStatusString(t *testing.T) {
	cases := []struct {
		s    CapabilityStatus
		want string
	}{
		{CapYes, "✓"},
		{CapNo, "—"},
		{CapUnknown, "?"},
		{CapabilityStatus(99), "?"},
	}
	for _, tc := range cases {
		if got := tc.s.String(); got != tc.want {
			t.Errorf("CapabilityStatus(%d).String() = %q, want %q", tc.s, got, tc.want)
		}
	}
}
