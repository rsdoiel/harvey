package harvey

import (
	"strings"
	"testing"
)

// The startup and confirmation prompts read with bufio.NewReaderSize(stdin, 1)
// on the belief that a 1-byte buffer stops the reader consuming bytes the REPL's line
// editor needs. bufio's minimum buffer is 16 bytes, so after reading `y\n` the reader
// had already swallowed the next 14 bytes of piped input. newLineReader takes exactly
// what it is asked for and no more.

func TestNewLineReader_DoesNotReadAhead(t *testing.T) {
	src := strings.NewReader("y\nsecond line the editor needs\nthird line\n")
	r := newLineReader(src)
	line, err := r.ReadString('\n')
	if err != nil || line != "y\n" {
		t.Fatalf("ReadString = %q, %v; want %q", line, err, "y\n")
	}
	if r.Buffered() != 0 {
		t.Errorf("the reader holds %d bytes it read ahead", r.Buffered())
	}
	if want := len("second line the editor needs\nthird line\n"); src.Len() != want {
		t.Errorf("%d bytes left in the source, want %d: the reader consumed more than the line", src.Len(), want)
	}
	// Whoever reads the source next gets the very next line, whole.
	if got := readLineFrom(src); got != "second line the editor needs" {
		t.Errorf("the next reader got %q", got)
	}
}

func TestNewLineReader_ReadsSeveralLinesInOrder(t *testing.T) {
	r := newLineReader(strings.NewReader("a\nbb\nccc"))
	for _, want := range []string{"a\n", "bb\n", "ccc"} {
		got, _ := r.ReadString('\n')
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	if _, err := r.ReadString('\n'); err == nil {
		t.Error("want an error at the end of input")
	}
}

// A prompt at the end of input must still report it, so the callers' end-of-input
// handling (a closed pipe is not "yes") keeps working.
func TestNewLineReader_EndOfInput(t *testing.T) {
	line, err := newLineReader(strings.NewReader("")).ReadString('\n')
	if err == nil || line != "" {
		t.Errorf("ReadString on empty input = %q, %v; want \"\" and an error", line, err)
	}
}
