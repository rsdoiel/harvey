package harvey

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// DefaultRetractionWatchURL is the base URL for the Retraction Watch API.
const DefaultRetractionWatchURL = "https://api.retractionwatch.com/api/v1/retractiondata/"

// retractionWatchEntry is one record returned by the Retraction Watch API.
type retractionWatchEntry struct {
	ArticleDOI     string `json:"ArticleDOI"`
	RetractionDate string `json:"RetractionDate"`
	Reason         string `json:"Reason"`
	Title          string `json:"Title"`
}

/** CheckDOIRetraction queries the Retraction Watch API for the given DOI and
 * reports whether the work has been retracted.
 *
 * Parameters:
 *   doi    (string) — DOI to query, e.g. "10.1234/example".
 *   apiURL (string) — base URL of the Retraction Watch API; use
 *                     DefaultRetractionWatchURL for production.
 *
 * Returns:
 *   retracted (bool)   — true when the DOI appears in the retraction database.
 *   note      (string) — human-readable note with reason and date; empty when
 *                        not retracted.
 *   error              — on network failure, non-200 response, or bad JSON.
 *
 * Example:
 *   retracted, note, err := CheckDOIRetraction("10.1234/paper", DefaultRetractionWatchURL)
 *   if retracted { fmt.Println("retracted:", note) }
 */
func CheckDOIRetraction(doi, apiURL string) (retracted bool, note string, err error) {
	queryURL := strings.TrimRight(apiURL, "/") + "/?ArticleDOI=" + url.QueryEscape(doi)
	resp, err := http.Get(queryURL) //nolint:noctx
	if err != nil {
		return false, "", fmt.Errorf("retraction check: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return false, "", fmt.Errorf("retraction check: server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var entries []retractionWatchEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return false, "", fmt.Errorf("retraction check: decode response: %w", err)
	}

	if len(entries) == 0 {
		return false, "", nil
	}

	e := entries[0]
	parts := []string{}
	if e.Reason != "" {
		parts = append(parts, e.Reason)
	}
	if e.RetractionDate != "" {
		parts = append(parts, e.RetractionDate)
	}
	n := strings.Join(parts, " (")
	if len(parts) == 2 {
		n += ")"
	}
	if n == "" {
		n = "Retracted (Retraction Watch)"
	}
	return true, n, nil
}
