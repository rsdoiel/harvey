package harvey

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// routedSession runs a scripted session in which the author sends a line to a
// registered route, here the fake Ollama under another name.
func routedSession(t *testing.T) (events []Event, rec, logPath string) {
	t.Helper()
	dir := scriptedRunFull(t, false, nil, false, func(a *Agent) {
		base := strings.TrimPrefix(a.Config.Ollama.URL, "http://")
		a.Routes = NewRouteRegistry()
		a.Routes.Add(&RouteEndpoint{Name: "pi2", URL: "ollama://" + base, Model: "llama3.2:3b", Kind: KindOllama})
		a.Routes.Enabled = true
	}, "@pi2 what is the weather there", "/exit")
	return logAndFountain(t, dir)
}

func TestRoutedTurn_TheLogHasTheInputTheRouteAndTheCall(t *testing.T) {
	events, _, _ := routedSession(t)
	var in, req, resp *Event
	for i := range events {
		switch events[i].Kind {
		case KindUserInput:
			in = &events[i]
		case KindModelRequest:
			if events[i].Level == LevelInfo {
				req = &events[i]
			}
		case KindModelResponse:
			resp = &events[i]
		}
	}
	if in == nil || req == nil || resp == nil {
		t.Fatalf("a routed turn should leave an input, a request and a response: %v %v %v", in != nil, req != nil, resp != nil)
	}
	var d struct {
		Text  Content `json:"text"`
		Route string  `json:"route"`
	}
	_ = json.Unmarshal(in.Data, &d)
	if d.Route != "pi2" || !strings.Contains(d.Text.Text, "what is the weather there") {
		t.Errorf("user_input = %s, want route pi2 and the line", in.Data)
	}
	if !strings.Contains(string(req.Data), `"form":"assembled"`) || !strings.Contains(string(resp.Data), `"status":"ok"`) {
		t.Errorf("request = %s / response = %s", req.Data, resp.Data)
	}
}

func TestRenderFountain_ARoutedTurn(t *testing.T) {
	events, _, lp := routedSession(t)
	got := string(RenderFountain(events, RenderOptions{Zone: time.Local, Blobs: BlobDir(lp)}))
	if !strings.Contains(got, "EXT. PI2 AND ") || !strings.Contains(got, "Harvey routing to PI2.") {
		t.Errorf("no exterior scene in the rendering:\n%s", got)
	}
	goldenCheck(t, "routed_turn", got)
}
