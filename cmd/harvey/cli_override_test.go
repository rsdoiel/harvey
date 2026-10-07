package main

import (
	"testing"

	harvey "github.com/rsdoiel/harvey"
)

// --ollama and -m/--model must be recorded as explicit so that harvey.yaml,
// loaded later by Run, does not overwrite them.
func TestFlags_OllamaAndModelAreMarkedExplicit(t *testing.T) {
	st := &startState{cfg: harvey.DefaultConfig()}
	for _, c := range []struct{ flag, v string }{{"--ollama", "http://x:1"}, {"-m", "m1"}} {
		if err := findFlag(c.flag).apply(st, c.v); err != nil {
			t.Fatal(err)
		}
	}
	if !st.cfg.Ollama.URLExplicit || !st.cfg.Ollama.ModelExplicit {
		t.Errorf("explicit flags = url:%v model:%v, want both true",
			st.cfg.Ollama.URLExplicit, st.cfg.Ollama.ModelExplicit)
	}
}
