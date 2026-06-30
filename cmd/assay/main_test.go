package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestNewAssayClient_ReturnsNonNilClient verifies that newAssayClient creates
// a valid LLMClient for the given base URL and model name.
func TestNewAssayClient_ReturnsNonNilClient(t *testing.T) {
	client := newAssayClient("http://localhost:11434", "test-model")
	if client == nil {
		t.Fatal("newAssayClient returned nil")
	}
	_ = client.Close()
}

// TestListOpenAIModels_ParsesModelIDs verifies that listOpenAIModels extracts
// model IDs from the /v1/models JSON response.
func TestListOpenAIModels_ParsesModelIDs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"object":"list","data":[{"id":"phi4-Q4_K_M"},{"id":"llama3.2:3b"}]}`)
	}))
	defer srv.Close()

	models, err := listOpenAIModels(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d: %v", len(models), models)
	}
	if models[0] != "phi4-Q4_K_M" {
		t.Errorf("expected phi4-Q4_K_M, got %s", models[0])
	}
	if models[1] != "llama3.2:3b" {
		t.Errorf("expected llama3.2:3b, got %s", models[1])
	}
}

// TestListOpenAIModels_SkipsEmptyIDs verifies that listOpenAIModels omits
// entries with an empty model ID.
func TestListOpenAIModels_SkipsEmptyIDs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"object":"list","data":[{"id":"phi4"},{"id":""}]}`)
	}))
	defer srv.Close()

	models, err := listOpenAIModels(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 1 || models[0] != "phi4" {
		t.Errorf("expected [phi4], got %v", models)
	}
}

// TestListOpenAIModels_ErrorOnBadJSON verifies that listOpenAIModels returns
// an error when the server responds with malformed JSON.
func TestListOpenAIModels_ErrorOnBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `not json`)
	}))
	defer srv.Close()

	_, err := listOpenAIModels(srv.URL)
	if err == nil {
		t.Fatal("expected error for bad JSON, got nil")
	}
}
