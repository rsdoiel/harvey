package harvey

import (
	"bytes"
	"strings"
	"testing"
)

func TestSelectFrom_emptyItems(t *testing.T) {
	var out bytes.Buffer
	got, err := SelectFrom(nil, "pick: ", strings.NewReader("1\n"), &out)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestSelectFrom_singleItem(t *testing.T) {
	items := []SelectItem{{Value: "only", Label: "only"}}
	var out bytes.Buffer
	got, err := SelectFrom(items, "pick: ", strings.NewReader(""), &out)
	if err != nil {
		t.Fatal(err)
	}
	if got != "only" {
		t.Errorf("expected %q, got %q", "only", got)
	}
	if out.Len() != 0 {
		t.Errorf("expected no output for single-item list, got %q", out.String())
	}
}

func TestSelectFrom_validIndex(t *testing.T) {
	items := []SelectItem{
		{Value: "alpha", Label: "alpha"},
		{Value: "beta", Label: "beta"},
		{Value: "gamma", Label: "gamma"},
	}
	var out bytes.Buffer
	got, err := SelectFrom(items, "pick: ", strings.NewReader("2\n"), &out)
	if err != nil {
		t.Fatal(err)
	}
	if got != "beta" {
		t.Errorf("expected %q, got %q", "beta", got)
	}
}

func TestSelectFrom_outOfRange(t *testing.T) {
	items := []SelectItem{
		{Value: "a", Label: "a"},
		{Value: "b", Label: "b"},
	}
	var out bytes.Buffer
	got, err := SelectFrom(items, "pick: ", strings.NewReader("99\n"), &out)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("out-of-range should cancel, got %q", got)
	}
}

func TestSelectFrom_emptyInput(t *testing.T) {
	items := []SelectItem{
		{Value: "x", Label: "x"},
		{Value: "y", Label: "y"},
	}
	var out bytes.Buffer
	got, err := SelectFrom(items, "pick: ", strings.NewReader("\n"), &out)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("empty input should cancel, got %q", got)
	}
}

func TestSelectFrom_cancelQ(t *testing.T) {
	items := []SelectItem{
		{Value: "x", Label: "x"},
		{Value: "y", Label: "y"},
	}
	var out bytes.Buffer
	got, err := SelectFrom(items, "pick: ", strings.NewReader("q\n"), &out)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("'q' should cancel, got %q", got)
	}
}

func TestSelectFrom_cancelZero(t *testing.T) {
	items := []SelectItem{
		{Value: "x", Label: "x"},
		{Value: "y", Label: "y"},
	}
	var out bytes.Buffer
	got, err := SelectFrom(items, "pick: ", strings.NewReader("0\n"), &out)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("'0' should cancel, got %q", got)
	}
}

func TestSelectFrom_rawInput(t *testing.T) {
	items := []SelectItem{
		{Value: "a", Label: "a"},
		{Value: "b", Label: "b"},
	}
	var out bytes.Buffer
	got, err := SelectFrom(items, "pick: ", strings.NewReader("/path/to/file\n"), &out)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/path/to/file" {
		t.Errorf("non-numeric input should pass through, got %q", got)
	}
}

func TestSelectFrom_activeMarker(t *testing.T) {
	items := []SelectItem{
		{Value: "a", Label: "alpha", Active: false},
		{Value: "b", Label: "beta", Active: true},
	}
	var out bytes.Buffer
	// cancel so we can just inspect the rendered list
	SelectFrom(items, "pick: ", strings.NewReader("\n"), &out) //nolint:errcheck
	rendered := out.String()
	if !strings.Contains(rendered, "→") {
		t.Error("active item should render with → marker")
	}
	if strings.Count(rendered, "→") != 1 {
		t.Errorf("expected exactly one → marker, got:\n%s", rendered)
	}
}

func TestSelectFromStrings_basic(t *testing.T) {
	items := []string{"foo", "bar", "baz"}
	var out bytes.Buffer
	got, err := SelectFromStrings(items, "pick: ", strings.NewReader("3\n"), &out)
	if err != nil {
		t.Fatal(err)
	}
	if got != "baz" {
		t.Errorf("expected %q, got %q", "baz", got)
	}
}

func TestSelectFromStrings_labelEqualsValue(t *testing.T) {
	items := []string{"tool_use", "workflow"}
	var out bytes.Buffer
	SelectFromStrings(items, "pick: ", strings.NewReader("\n"), &out) //nolint:errcheck
	rendered := out.String()
	if !strings.Contains(rendered, "tool_use") {
		t.Error("label should equal value in SelectFromStrings")
	}
}

func TestParseSelectInput_validNumber(t *testing.T) {
	idx, raw := parseSelectInput("2", 5)
	if idx != 2 || raw != "" {
		t.Errorf("expected (2, \"\"), got (%d, %q)", idx, raw)
	}
}

func TestParseSelectInput_outOfRange(t *testing.T) {
	idx, raw := parseSelectInput("10", 5)
	if idx != 0 || raw != "" {
		t.Errorf("expected (0, \"\"), got (%d, %q)", idx, raw)
	}
}

func TestParseSelectInput_empty(t *testing.T) {
	idx, raw := parseSelectInput("", 5)
	if idx != 0 || raw != "" {
		t.Errorf("expected (0, \"\"), got (%d, %q)", idx, raw)
	}
}

func TestParseSelectInput_q(t *testing.T) {
	idx, raw := parseSelectInput("q", 5)
	if idx != 0 || raw != "" {
		t.Errorf("expected (0, \"\"), got (%d, %q)", idx, raw)
	}
}

func TestParseSelectInput_rawString(t *testing.T) {
	idx, raw := parseSelectInput("my/path", 5)
	if idx != 0 || raw != "my/path" {
		t.Errorf("expected (0, \"my/path\"), got (%d, %q)", idx, raw)
	}
}
