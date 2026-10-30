package harvey

import (
	"strings"
	"testing"
)

const testPEM = "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEAx\nabc123def456\n-----END RSA PRIVATE KEY-----"

func TestScrubEvent_NormalisesWorkspacePath(t *testing.T) {
	got, rep := ScrubEvent("open /home/alice/proj/src/main.go", "/home/alice/proj")
	if want := "open <workspace>/src/main.go"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if len(rep.Redactions) != 0 {
		t.Errorf("a path is normalised, not redacted: %v", rep.Redactions)
	}
}

func TestScrubEvent_IgnoresEmptyAndRootWorkspace(t *testing.T) {
	in := "cat /etc/hosts"
	for _, ws := range []string{"", "/"} {
		if got, _ := ScrubEvent(in, ws); got != in {
			t.Errorf("workspace %q: got %q, want text unchanged", ws, got)
		}
	}
}

func TestScrubEvent_RedactsPEMBlockWhole(t *testing.T) {
	in := "key follows:\n" + testPEM + "\nend of key"
	got, rep := ScrubEvent(in, "")
	if want := "key follows:\n[REDACTED:pem]\nend of key"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if rep.Redactions["pem"] != 1 {
		t.Errorf("Redactions = %v, want one pem", rep.Redactions)
	}
	if strings.Contains(got, "MIIEow") || strings.Contains(got, "abc123") {
		t.Errorf("key material survived: %q", got)
	}
}

func TestScrubEvent_RedactsUnterminatedPEMToEnd(t *testing.T) {
	in := "before\n-----BEGIN PRIVATE KEY-----\nMIIE\nstill key material"
	got, _ := ScrubEvent(in, "")
	if want := "before\n[REDACTED:pem]"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestScrubEvent_RedactsPGPPrivateKeyBlock(t *testing.T) {
	in := "-----BEGIN PGP PRIVATE KEY BLOCK-----\nlQOYBF\n-----END PGP PRIVATE KEY BLOCK-----"
	got, rep := ScrubEvent(in, "")
	if got != "[REDACTED:pem]" || rep.Redactions["pem"] != 1 {
		t.Errorf("got %q, %v; want the whole block redacted", got, rep.Redactions)
	}
}

func TestScrubEvent_RedactsAdjacentQuotedSegments(t *testing.T) {
	got, _ := ScrubEvent("password = 'it''s'", "")
	if want := "password = [REDACTED:secret]"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	got, _ = ScrubEvent(`{"password":"a b"}`, "")
	if want := `{"password":[REDACTED:secret]}`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestScrubEvent_LeavesCertificateBlockAlone(t *testing.T) {
	in := "-----BEGIN CERTIFICATE-----\nMIIC\n-----END CERTIFICATE-----"
	got, rep := ScrubEvent(in, "")
	if got != in {
		t.Errorf("a public certificate is not a secret; got %q", got)
	}
	if rep.Redactions["pem"] != 0 {
		t.Errorf("Redactions = %v", rep.Redactions)
	}
}

func TestScrubEvent_AuthorizationHeaderKeepsNameLosesValue(t *testing.T) {
	got, rep := ScrubEvent("curl -H 'Authorization: Bearer abc.def-123' https://x", "")
	if want := "curl -H 'Authorization: [REDACTED:authorization]' https://x"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if rep.Redactions["authorization"] != 1 {
		t.Errorf("Redactions = %v", rep.Redactions)
	}
	if strings.Contains(got, "abc.def") {
		t.Errorf("value survived: %q", got)
	}
}

func TestScrubEvent_AuthorizationBasicAndLineEnd(t *testing.T) {
	got, _ := ScrubEvent("Authorization: Basic dXNlcjpwYXNz\nnext line", "")
	if want := "Authorization: [REDACTED:authorization]\nnext line"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestScrubEvent_BareBearerValue(t *testing.T) {
	got, rep := ScrubEvent("send Bearer abc123XYZ in the header", "")
	if want := "send Bearer [REDACTED:bearer] in the header"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if rep.Redactions["bearer"] != 1 {
		t.Errorf("Redactions = %v", rep.Redactions)
	}
}

func TestScrubEvent_SecretNamesKeepNameLoseValue(t *testing.T) {
	cases := []struct{ in, want string }{
		{"API_KEY=xyz run", "API_KEY=[REDACTED:secret] run"},
		{"export GITHUB_TOKEN=ghp_abc123", "export GITHUB_TOKEN=[REDACTED:secret]"},
		{"AWS_SECRET_ACCESS_KEY=wJalr", "AWS_SECRET_ACCESS_KEY=[REDACTED:secret]"},
		{"password: hunter2", "password: [REDACTED:secret]"},
		{"DB_PASSWORD = \"p w d\"", "DB_PASSWORD = [REDACTED:secret]"},
		{`{"api-key": "k-1", "n": 1}`, `{"api-key": [REDACTED:secret], "n": 1}`},
		{"passwd='abc'", "passwd=[REDACTED:secret]"},
		{"client_secret=zzz", "client_secret=[REDACTED:secret]"},
		{"private_key: abcdef", "private_key: [REDACTED:secret]"},
		{"access-token=abc", "access-token=[REDACTED:secret]"},
		{"token=abc", "token=[REDACTED:secret]"},
	}
	for _, c := range cases {
		got, rep := ScrubEvent(c.in, "")
		if got != c.want {
			t.Errorf("ScrubEvent(%q) = %q, want %q", c.in, got, c.want)
		}
		if rep.Redactions["secret"] != 1 {
			t.Errorf("ScrubEvent(%q): Redactions = %v, want one secret", c.in, rep.Redactions)
		}
	}
}

func TestScrubEvent_EmptyValueDoesNotEatNextLine(t *testing.T) {
	in := "password:\nusername: bob"
	got, rep := ScrubEvent(in, "")
	if got != in {
		t.Errorf("got %q, want text unchanged (no value on the line)", got)
	}
	if rep.Redactions["secret"] != 0 {
		t.Errorf("Redactions = %v", rep.Redactions)
	}
}

func TestScrubEvent_ProseAboutTokensIsUntouched(t *testing.T) {
	cases := []string{
		"the token limit is 4096",
		"max_tokens=512",
		"set the secrets file permissions",
		"passwords are hashed",
		"tokenizer=bpe",
		"mytoken=abc",
		"token_count: 12",
		"secrets: [a, b]",
		"the bearer of the key",
	}
	for _, in := range cases {
		got, rep := ScrubEvent(in, "")
		if got != in {
			t.Errorf("ScrubEvent(%q) = %q, want unchanged", in, got)
		}
		if n := totalRedactions(rep); n != 0 {
			t.Errorf("ScrubEvent(%q): %d redactions, want 0", in, n)
		}
	}
}

func TestScrubEvent_BareWordsAreFlaggedNotRedacted(t *testing.T) {
	in := "remember to rotate the password and the api_key someday"
	got, rep := ScrubEvent(in, "")
	if got != in {
		t.Errorf("a bare word must not be redacted; got %q", got)
	}
	want := map[string]bool{`contains "password"`: true, `contains "api_key"`: true}
	for _, f := range rep.Flags {
		delete(want, f)
	}
	if len(want) != 0 {
		t.Errorf("missing flags %v; got %v", want, rep.Flags)
	}
}

func TestScrubEvent_RedactedTextIsNotFlaggedAgain(t *testing.T) {
	_, rep := ScrubEvent("Authorization: Bearer abc", "")
	for _, f := range rep.Flags {
		t.Errorf("flag %q raised for text that was redacted", f)
	}
}

func TestScrubEvent_ReportNeverContainsTheValue(t *testing.T) {
	_, rep := ScrubEvent("PASSWORD=hunter2 and "+testPEM, "")
	if s := rep.String(); strings.Contains(s, "hunter2") || strings.Contains(s, "MIIEow") {
		t.Errorf("report leaks a value: %q", s)
	}
	if s := rep.String(); !strings.Contains(s, "secret") || !strings.Contains(s, "pem") {
		t.Errorf("report should name the classes: %q", s)
	}
}

func TestScrubEvent_ReportOfCleanTextIsEmpty(t *testing.T) {
	_, rep := ScrubEvent("nothing to see", "")
	if rep.String() != "" || totalRedactions(rep) != 0 || len(rep.Flags) != 0 {
		t.Errorf("clean text produced a report: %+v", rep)
	}
}

func TestScrubEvent_IsIdempotent(t *testing.T) {
	in := strings.Join([]string{
		"open /home/alice/proj/x",
		"Authorization: Bearer abc",
		"send Bearer def",
		"API_KEY=xyz and token=t1",
		testPEM,
		"the password is a flagged bare word",
	}, "\n")
	once, _ := ScrubEvent(in, "/home/alice/proj")
	twice, rep := ScrubEvent(once, "/home/alice/proj")
	if once != twice {
		t.Errorf("not idempotent:\nonce:  %q\ntwice: %q", once, twice)
	}
	if n := totalRedactions(rep); n != 0 {
		t.Errorf("second pass redacted %d more values", n)
	}
}

func TestScrubEvent_CountsEachRedaction(t *testing.T) {
	_, rep := ScrubEvent("A_TOKEN=1\nB_SECRET=2\nBearer xyz", "")
	if rep.Redactions["secret"] != 2 || rep.Redactions["bearer"] != 1 {
		t.Errorf("Redactions = %v, want secret 2 and bearer 1", rep.Redactions)
	}
}

func TestScrubEvent_MultipleClassesInOneText(t *testing.T) {
	in := "Authorization: Bearer a\nAPI_KEY=b\n" + testPEM
	got, _ := ScrubEvent(in, "")
	for _, leak := range []string{"Bearer a", "=b", "MIIEow"} {
		if strings.Contains(got, leak) {
			t.Errorf("%q survived in %q", leak, got)
		}
	}
}

func totalRedactions(r ScrubReport) int {
	n := 0
	for _, c := range r.Redactions {
		n += c
	}
	return n
}
