package harvey

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"
)

// hailoDevicePath is the device node the Hailo driver creates for a card.
const hailoDevicePath = "/dev/hailo0"

// hailoListPath is hailo-ollama's catalog route. It exists on no other server,
// so a well-formed answer identifies hailo-ollama; regular Ollama answers 404.
const hailoListPath = "/hailo/v1/list"

// hailoProbeTimeout bounds the one request DetectHailo makes.
const hailoProbeTimeout = 2 * time.Second

/** HailoStatus is what DetectHailo found. The card and the server are separate
 * facts: a card with the service stopped, and a server on another machine, are
 * both real cases.
 *
 * Fields:
 *   CardPresent (bool)     — the Hailo device node exists on this machine.
 *   Probed      (bool)     — a request was made; false means none was tried.
 *   URL         (string)   — the URL probed; "" when Probed is false.
 *   ServerUp    (bool)     — URL answered /hailo/v1/list in the expected shape.
 *   Catalog     ([]string) — model names the server can run; empty unless ServerUp.
 *
 * Example:
 *   st := DetectHailo(cfg.Hailo, "")
 *   if st.CardPresent && !st.ServerUp { fmt.Println("AI HAT+ 2 found, hailo-ollama is not running") }
 */
type HailoStatus struct {
	CardPresent bool
	Probed      bool
	URL         string
	ServerUp    bool
	Catalog     []string
}

/** DetectHailo looks for a Hailo card and a hailo-ollama server. It probes the
 * server only when a card is present or cfg.URLSet is true; with neither it
 * makes no request, prints nothing and waits for nothing, so a machine without
 * the HAT pays no cost. An answer that is not 200, not JSON, or not
 * {"models": [strings]} means "not Hailo", never an error.
 *
 * Parameters:
 *   cfg        (HailoConfig) — the hailo: section of harvey.yaml.
 *   devicePath (string)      — the card's device node; "" = /dev/hailo0.
 *
 * Returns:
 *   HailoStatus — what was found; see its fields.
 *
 * Example:
 *   st := DetectHailo(cfg.Hailo, "")
 *   if st.ServerUp { fmt.Println(len(st.Catalog), "models in the Hailo catalog") }
 */
func DetectHailo(cfg HailoConfig, devicePath string) HailoStatus {
	if devicePath == "" {
		devicePath = hailoDevicePath
	}
	var st HailoStatus
	if _, err := os.Stat(devicePath); err == nil {
		st.CardPresent = true
	}
	if !st.CardPresent && !cfg.URLSet {
		return st
	}
	st.Probed = true
	st.URL = cfg.URL
	st.Catalog = fetchHailoCatalog(cfg.URL)
	st.ServerUp = st.Catalog != nil
	return st
}

// fetchHailoCatalog returns the model names hailo-ollama at baseURL can run,
// or nil when nothing there answers /hailo/v1/list in the expected shape. An
// empty catalog from a real server is a non-nil empty slice.
func fetchHailoCatalog(baseURL string) []string {
	c := &http.Client{Timeout: hailoProbeTimeout}
	resp, err := c.Get(strings.TrimRight(baseURL, "/") + hailoListPath)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var body struct {
		Models []string `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.Models == nil {
		return nil
	}
	return body.Models
}
