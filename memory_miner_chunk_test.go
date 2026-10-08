package harvey

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// sizeLimitedClient answers a mining request with one memory, unless the
// session text in the request is longer than limit characters, when it fails
// the way hailo-ollama does with an oversize prompt. It records the size of
// every request.
type sizeLimitedClient struct {
	name  string
	limit int
	same  bool // true: every reply proposes the same memory
	sizes []int
	fails int
	texts []string
}

func (c *sizeLimitedClient) Name() string                               { return c.name }
func (c *sizeLimitedClient) Models(_ context.Context) ([]string, error) { return nil, nil }
func (c *sizeLimitedClient) Close() error                               { return nil }
func (c *sizeLimitedClient) Chat(_ context.Context, msgs []Message, out io.Writer) (ChatStats, error) {
	text := msgs[1].Content
	c.sizes = append(c.sizes, len(text))
	if len(text) > c.limit {
		c.fails++
		return ChatStats{}, fmt.Errorf("chat: %w", ErrStreamTruncated)
	}
	c.texts = append(c.texts, text)
	desc := fmt.Sprintf("Lesson number %d from the session.", len(c.texts))
	if c.same {
		desc = "Always run go test from inside harvey/."
	}
	fmt.Fprintf(out, `[{"type":"workflow","kind":"recommendation","description":%q,"summary":"s","action":"a","tags":["go"],"fountain_body":"FADE IN:\n\nTHE END.\n"}]`, desc)
	return ChatStats{}, nil
}

// longSession is a recorded session with n question and answer turns, about
// 200 characters each, so the dialogue is far larger than a small prompt limit.
func longSession(n int) string {
	var b strings.Builder
	b.WriteString("Title: Harvey Session\nAuthor: RSDOIEL\nDate: 2026-10-08 09:00:00\n\nFADE IN:\n\nINT. HARVEY AND RSDOIEL TALKING 2026-10-08 09:00:00\n\n")
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "RSDOIEL\nQuestion %03d: %s\n\nHARVEY\nAnswer %03d: %s\n\n", i, strings.Repeat("why ", 12), i, strings.Repeat("because ", 8))
	}
	b.WriteString("THE END.\n")
	return b.String()
}

func TestTakeChunk_PacksParagraphsAndLosesNothing(t *testing.T) {
	text := longSession(30)
	var parts []string
	rest := text
	for rest != "" {
		var chunk string
		chunk, rest = takeChunk(rest, 150) // 150 tokens = 600 bytes
		if chunk == "" {
			t.Fatal("takeChunk returned an empty chunk, which would loop forever")
		}
		if len(chunk) > 600 {
			t.Errorf("chunk of %d bytes exceeds the 600 byte budget", len(chunk))
		}
		parts = append(parts, chunk)
	}
	if len(parts) < 5 {
		t.Errorf("a %d byte session in 600 byte chunks should be several parts, got %d", len(text), len(parts))
	}
	if strings.Join(parts, "") != text {
		t.Error("the chunks do not add up to the session: text was lost or repeated")
	}
	for i, p := range parts[:len(parts)-1] {
		if !strings.HasSuffix(p, "\n\n") {
			t.Errorf("chunk %d does not end at a paragraph boundary: %q", i, p[max(0, len(p)-30):])
		}
	}
}

func TestTakeChunk_WholeTextWhenItFitsOrNoLimit(t *testing.T) {
	text := longSession(3)
	for _, size := range []int{0, -1, 100000} {
		chunk, rest := takeChunk(text, size)
		if chunk != text || rest != "" {
			t.Errorf("size %d: want the whole text, got %d bytes and rest %d", size, len(chunk), len(rest))
		}
	}
}

func TestTakeChunk_SplitsAnOversizeParagraphByLines(t *testing.T) {
	var lines []string
	for i := 0; i < 40; i++ {
		lines = append(lines, fmt.Sprintf("line %02d %s", i, strings.Repeat("x", 30)))
	}
	text := strings.Join(lines, "\n") + "\n" // one paragraph, about 1500 bytes
	var parts []string
	rest := text
	for rest != "" {
		var chunk string
		chunk, rest = takeChunk(rest, 100) // 400 bytes
		if len(chunk) > 400 || chunk == "" {
			t.Fatalf("bad chunk of %d bytes", len(chunk))
		}
		parts = append(parts, chunk)
	}
	if strings.Join(parts, "") != text {
		t.Error("text lost splitting an oversize paragraph")
	}
	for i, p := range parts {
		if !strings.HasSuffix(p, "\n") {
			t.Errorf("chunk %d was cut mid-line", i)
		}
	}
}

func TestTakeChunk_ALineLongerThanTheBudgetIsCutNotLooped(t *testing.T) {
	text := strings.Repeat("é", 1000) // one line, no newline, multibyte
	var n int
	rest := text
	for rest != "" {
		var chunk string
		chunk, rest = takeChunk(rest, 50) // 200 bytes
		if chunk == "" || n > 100 {
			t.Fatal("no progress")
		}
		if !strings.HasPrefix(text, strings.Repeat("é", 1)) || strings.ContainsRune(chunk, '�') {
			t.Fatalf("a rune was cut in half: %q", chunk)
		}
		n++
	}
}

func TestMinerChunkTokens(t *testing.T) {
	if n, err := minerChunkTokens(0); err != nil || n != 0 {
		t.Errorf("unknown limit: want 0 (no cap), got %d, %v", n, err)
	}
	n, err := minerChunkTokens(2000)
	if err != nil || n <= 0 || n >= 2000 {
		t.Errorf("limit 2000: want room left after the instructions, got %d, %v", n, err)
	}
	if _, err := minerChunkTokens(700); err == nil {
		t.Error("a limit that cannot hold the instructions plus a useful amount of dialogue must be an error")
	}
}

func TestMineAuto_SessionThatFits_OneCallNoChunking(t *testing.T) {
	miner, _, manifest, sess := minerFixture(t, longSession(3))
	client := &sizeLimitedClient{name: "m1", limit: 1 << 20}
	var out bytes.Buffer

	if err := miner.MineAuto(context.Background(), sess, &Agent{Client: client}, nil, &out); err != nil {
		t.Fatalf("MineAuto: %v", err)
	}
	if len(client.sizes) != 1 {
		t.Errorf("a session that fits should take one call, took %d", len(client.sizes))
	}
	if strings.Contains(out.String(), "parts") {
		t.Errorf("no chunking message expected: %q", out.String())
	}
	if !manifest.IsMined(sess) {
		t.Error("not recorded as mined")
	}
}

func TestMineAuto_LargeSession_ReadInPartsWhenTheModelFailsOnTheWhole(t *testing.T) {
	text := longSession(40) // about 9 KB
	miner, store, manifest, sess := minerFixture(t, text)
	client := &sizeLimitedClient{name: "m1", limit: 2500}
	var out bytes.Buffer

	if err := miner.MineAuto(context.Background(), sess, &Agent{Client: client}, nil, &out); err != nil {
		t.Fatalf("MineAuto: %v", err)
	}
	if !manifest.IsMined(sess) || manifest.FailedOn(sess, "m1") {
		t.Fatal("a session this model can read in parts is mined, not failed")
	}
	// Everything was read: every turn appears in some request that succeeded.
	all := strings.Join(client.texts, "")
	for i := 1; i <= 40; i++ {
		if !strings.Contains(all, fmt.Sprintf("Question %03d", i)) || !strings.Contains(all, fmt.Sprintf("Answer %03d", i)) {
			t.Fatalf("turn %d never reached the model", i)
		}
	}
	// What failed once is not tried again at that size: whole (9 KB), then half.
	if client.fails > 3 {
		t.Errorf("the size that failed was retried: %d failures (sizes %v)", client.fails, client.sizes)
	}
	metas, err := store.List("")
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) < 2 {
		t.Errorf("proposals from the parts should all be saved, got %d memories", len(metas))
	}
	if !strings.Contains(out.String(), "parts") {
		t.Errorf("the user should be told the session was read in parts: %q", out.String())
	}
}

func TestMineAuto_ModelThatFailsAtEverySize_NotMinedAndTerminates(t *testing.T) {
	miner, store, manifest, sess := minerFixture(t, longSession(40))
	client := &sizeLimitedClient{name: "m1", limit: 10}

	err := miner.MineAuto(context.Background(), sess, &Agent{Client: client}, nil, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "could not be processed") {
		t.Fatalf("want a could-not-be-processed error, got %v", err)
	}
	if manifest.IsMined(sess) || !manifest.FailedOn(sess, "m1") {
		t.Error("not mined, and the failure is remembered")
	}
	if len(client.sizes) > 20 {
		t.Errorf("gave up too slowly: %d requests", len(client.sizes))
	}
	if metas, _ := store.List(""); len(metas) != 0 {
		t.Errorf("a session that could not be processed saves nothing, got %d memories", len(metas))
	}
}

func TestMine_ChunkedProposalsAreMergedAndDeduplicated(t *testing.T) {
	miner, _, manifest, sess := minerFixture(t, longSession(40))
	client := &sizeLimitedClient{name: "m1", limit: 2500, same: true}
	var out bytes.Buffer

	if err := miner.Mine(context.Background(), sess, &Agent{Client: client}, nil, &out, strings.NewReader("a\n")); err != nil {
		t.Fatalf("Mine: %v", err)
	}
	if !strings.Contains(out.String(), "Proposed memory 1 of 1") {
		t.Errorf("the same lesson from every part should be proposed once:\n%s", out.String())
	}
	if !manifest.IsMined(sess) {
		t.Error("not mined")
	}
}

func TestMineAuto_ABadReplyInALaterPartLeavesTheSessionUnmined(t *testing.T) {
	miner, store, manifest, sess := minerFixture(t, longSession(40))
	client := &partFailingClient{inner: sizeLimitedClient{name: "m1", limit: 2500}, badFrom: 3}

	err := miner.MineAuto(context.Background(), sess, &Agent{Client: client}, nil, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "could not be processed") {
		t.Fatalf("want a could-not-be-processed error, got %v", err)
	}
	if manifest.IsMined(sess) {
		t.Error("a half-read session must not be recorded as mined")
	}
	if metas, _ := store.List(""); len(metas) != 0 {
		t.Errorf("nothing from a half-read session is saved, got %d", len(metas))
	}
}

// partFailingClient behaves like sizeLimitedClient until its badFrom-th
// successful reply, after which it answers with prose.
type partFailingClient struct {
	inner   sizeLimitedClient
	badFrom int
}

func (c *partFailingClient) Name() string                               { return c.inner.name }
func (c *partFailingClient) Models(_ context.Context) ([]string, error) { return nil, nil }
func (c *partFailingClient) Close() error                               { return nil }
func (c *partFailingClient) Chat(ctx context.Context, msgs []Message, out io.Writer) (ChatStats, error) {
	if len(c.inner.texts) >= c.badFrom-1 && len(msgs[1].Content) <= c.inner.limit {
		_, _ = io.WriteString(out, "Sorry, I cannot help with that.")
		return ChatStats{}, nil
	}
	return c.inner.Chat(ctx, msgs, out)
}

// blockingWriter accepts limit bytes and then blocks forever, so a loop that
// prints without end stalls instead of eating memory.
type blockingWriter struct {
	n, limit int
	stall    chan struct{}
}

func (w *blockingWriter) Write(p []byte) (int, error) {
	w.n += len(p)
	if w.n > w.limit {
		<-w.stall
	}
	return len(p), nil
}

// Found while testing chunking: when input ended during the review, the prompt
// read an empty line, called it an unknown command, and asked again, for ever.
func TestMine_ReviewStopsAtEndOfInput(t *testing.T) {
	miner, _, manifest, sess := minerFixture(t, longSession(40))
	client := &sizeLimitedClient{name: "m1", limit: 2500} // several proposals, one answer given
	w := &blockingWriter{limit: 1 << 20, stall: make(chan struct{})}
	defer close(w.stall)

	done := make(chan error, 1)
	go func() {
		done <- miner.Mine(context.Background(), sess, &Agent{Client: client}, nil, w, strings.NewReader("a\n"))
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Mine: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the review kept prompting after input ended")
	}
	if manifest.IsMined(sess) {
		t.Error("a review cut short by end of input must leave the session to be offered again")
	}
}

// chatSizeServer is an Ollama-style server that answers every chat with an
// empty list of memories and records the size of the session text it was sent.
type chatSizeServer struct {
	*httptest.Server
	mu    sync.Mutex
	sizes []int
}

func newChatSizeServer(t *testing.T) *chatSizeServer {
	t.Helper()
	cs := &chatSizeServer{}
	cs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		n := 0
		for _, m := range body.Messages {
			if m.Role == "user" {
				n = len(m.Content)
			}
		}
		cs.mu.Lock()
		cs.sizes = append(cs.sizes, n)
		cs.mu.Unlock()
		w.Header().Set("Content-Type", "application/x-ndjson")
		io.WriteString(w, `{"model":"m","message":{"role":"assistant","content":"[]"},"done":false}`+"\n")
		io.WriteString(w, `{"model":"m","message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","eval_count":1}`+"\n")
	}))
	t.Cleanup(cs.Close)
	return cs
}

// limitedAgent is an agent whose real client talks to srv and whose active
// model has a cached prompt limit.
func limitedAgent(t *testing.T, srv *chatSizeServer, limit int) *Agent {
	t.Helper()
	a, mc := newTestAgentWithCache(t, "llama3.2:3b")
	if err := mc.Set(&ModelCapability{Name: "llama3.2:3b", MaxPromptTokens: limit, ProbeLevel: "fast", ProbedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	a.Client = newOllamaLLMClient(srv.URL, "llama3.2:3b", 0)
	return a
}

func TestMineAuto_KnownPromptLimit_SizesThePartsUpFront(t *testing.T) {
	miner, _, manifest, sess := minerFixture(t, longSession(40)) // about 9 KB
	srv := newChatSizeServer(t)
	a := limitedAgent(t, srv, 2000) // room for about 5 KB of dialogue after the instructions

	if err := miner.MineAuto(context.Background(), sess, a, nil, io.Discard); err != nil {
		t.Fatalf("MineAuto: %v", err)
	}
	if !manifest.IsMined(sess) {
		t.Fatal("not mined")
	}
	if len(srv.sizes) < 2 || len(srv.sizes) > 3 {
		t.Errorf("a 9 KB session under a 2000 token limit should take 2 or 3 requests, took %d: %v", len(srv.sizes), srv.sizes)
	}
	for _, n := range srv.sizes {
		if n > 5500 {
			t.Errorf("a request carried %d bytes of dialogue, past what the limit allows", n)
		}
	}
}

func TestMineAuto_PromptLimitTooSmall_NothingSentAndReported(t *testing.T) {
	miner, _, manifest, sess := minerFixture(t, longSession(10))
	srv := newChatSizeServer(t)
	a := limitedAgent(t, srv, 700)

	err := miner.MineAuto(context.Background(), sess, a, nil, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "could not be processed") || !strings.Contains(err.Error(), "700") {
		t.Fatalf("want a could-not-be-processed error naming the limit, got %v", err)
	}
	if len(srv.sizes) != 0 {
		t.Errorf("no request should be sent to a model that cannot take the instructions, sent %d", len(srv.sizes))
	}
	if manifest.IsMined(sess) || !manifest.FailedOn(sess, minerModel(a)) {
		t.Error("not mined, and the failure is remembered")
	}
}

func TestMineAuto_LaterPartsSayTheyAreContinuations(t *testing.T) {
	miner, _, _, sess := minerFixture(t, longSession(40))
	client := &sizeLimitedClient{name: "m1", limit: 2500}
	if err := miner.MineAuto(context.Background(), sess, &Agent{Client: client}, nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(client.texts) < 2 {
		t.Fatalf("expected several parts, got %d", len(client.texts))
	}
	if strings.HasPrefix(client.texts[0], minerContinuation) {
		t.Error("the first part is not a continuation")
	}
	for i, txt := range client.texts[1:] {
		if !strings.HasPrefix(txt, minerContinuation) {
			t.Errorf("part %d does not say it continues a transcript", i+2)
		}
	}
}

// proseOnBigClient rambles in prose when the session text is longer than limit
// characters, as a small model does on a long input, and answers properly on
// anything smaller.
type proseOnBigClient struct {
	inner sizeLimitedClient
	limit int
	prose int
}

func (c *proseOnBigClient) Name() string                               { return c.inner.name }
func (c *proseOnBigClient) Models(_ context.Context) ([]string, error) { return nil, nil }
func (c *proseOnBigClient) Close() error                               { return nil }
func (c *proseOnBigClient) Chat(ctx context.Context, msgs []Message, out io.Writer) (ChatStats, error) {
	if len(msgs[1].Content) > c.limit {
		c.prose++
		_, _ = io.WriteString(out, "This session is about many things, let me summarise them in prose.")
		return ChatStats{}, nil
	}
	return c.inner.Chat(ctx, msgs, out)
}

func TestMineAuto_ProseOnALongPart_TriesSmallerParts(t *testing.T) {
	miner, _, manifest, sess := minerFixture(t, longSession(40))
	client := &proseOnBigClient{inner: sizeLimitedClient{name: "m1", limit: 1 << 20}, limit: 2500}

	if err := miner.MineAuto(context.Background(), sess, &Agent{Client: client}, nil, io.Discard); err != nil {
		t.Fatalf("a model that copes with smaller parts should mine the session: %v", err)
	}
	if !manifest.IsMined(sess) || manifest.FailedOn(sess, "m1") {
		t.Error("should be mined, not failed")
	}
	if client.prose > 4 {
		t.Errorf("the size that produced prose was retried: %d prose replies", client.prose)
	}
}
