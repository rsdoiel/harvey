package harvey

import (
	"strings"
	"testing"
)

func TestDeprecationNotice_OncePerKey(t *testing.T) {
	resetDeprecationNotices()
	var out strings.Builder
	deprecationNotice(&out, "k", "hello")
	deprecationNotice(&out, "k", "hello")
	deprecationNotice(&out, "other", "bye")
	if got := strings.Count(out.String(), "hello"); got != 1 {
		t.Errorf("notice shown %d times, want 1:\n%s", got, out.String())
	}
	if !strings.Contains(out.String(), "bye") {
		t.Errorf("a different key must still show:\n%s", out.String())
	}
	deprecationNotice(nil, "nil", "ignored") // must not panic
}

func TestMemoryNotice_NamesWhatToDoInstead(t *testing.T) {
	resetDeprecationNotices()
	var out strings.Builder
	noteMemoryRetiring(&out)
	for _, want := range []string{"DR-0037", "/kb observation add", "agents/memories/", "CHANGES.md"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("notice missing %q:\n%s", want, out.String())
		}
	}
}

func TestMemoryNotice_MemoryCommandShowsItOnce(t *testing.T) {
	resetDeprecationNotices()
	a := newTestAgent(t)
	var out strings.Builder
	_ = cmdMemory(a, []string{"list"}, &out)
	_ = cmdMemory(a, []string{"list"}, &out)
	if got := strings.Count(out.String(), "memory store is deprecated"); got != 1 {
		t.Errorf("notice shown %d times, want 1:\n%s", got, out.String())
	}
}

func TestMemoryNotice_StartupOnlyWhenTheStoreHoldsMemories(t *testing.T) {
	resetDeprecationNotices()
	a := newTestAgent(t)
	ms, err := OpenMemory(a.Workspace, &a.Config.Memory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ms.Close() })
	a.Memory = ms

	var out strings.Builder
	a.startupDeprecations(&out)
	if out.Len() != 0 {
		t.Errorf("an empty store must not be announced, got %q", out.String())
	}

	doc := &MemoryDoc{Meta: MemoryMeta{ID: "m1", Type: MemoryTypeToolUse, Description: "d", Summary: "s", Confidence: 0.5}}
	if err := ms.Store.Save(doc, nil); err != nil {
		t.Fatal(err)
	}
	a.startupDeprecations(&out)
	if !strings.Contains(out.String(), "memory store is deprecated") {
		t.Errorf("a store with a memory must be announced, got %q", out.String())
	}
}
