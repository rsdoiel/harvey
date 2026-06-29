package harvey

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCheckDOIRetraction_NotRetracted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("ArticleDOI"); got != "10.1234/clean" {
			t.Errorf("expected ArticleDOI=10.1234/clean, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	retracted, note, err := CheckDOIRetraction("10.1234/clean", srv.URL+"/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if retracted {
		t.Error("expected retracted=false for empty response")
	}
	if note != "" {
		t.Errorf("expected empty note, got %q", note)
	}
}

func TestCheckDOIRetraction_Retracted(t *testing.T) {
	entry := retractionWatchEntry{
		ArticleDOI:     "10.1234/bad",
		RetractionDate: "2024/03/15",
		Reason:         "Data fabrication",
		Title:          "Problematic Paper",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]retractionWatchEntry{entry})
	}))
	defer srv.Close()

	retracted, note, err := CheckDOIRetraction("10.1234/bad", srv.URL+"/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !retracted {
		t.Error("expected retracted=true")
	}
	if !strings.Contains(note, "Data fabrication") {
		t.Errorf("note should contain reason, got %q", note)
	}
	if !strings.Contains(note, "2024/03/15") {
		t.Errorf("note should contain date, got %q", note)
	}
}

func TestCheckDOIRetraction_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, _, err := CheckDOIRetraction("10.1234/any", srv.URL+"/")
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestCheckDOIRetraction_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	_, _, err := CheckDOIRetraction("10.1234/any", srv.URL+"/")
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}
