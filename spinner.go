package harvey

import (
	"fmt"
	"io"
	"time"
)

// spinnerFrames are the braille animation frames for the spinner.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// learColors is a palette of legible foreground colors cycled across the
// Edward Lear messages. Bright variants (9x) are chosen so they read
// clearly on both dark and light terminal backgrounds.
var learColors = []func(string) string{
	cyan,
	yellow,
	green,
	magenta,
	blue,
	red,
}

// Spinner displays an animated 3-line block while the LLM backend processes
// a request:
//
//	<model source and name>
//	  ⎿ <Edward Lear message>      (updates every ~6 s)
//	     ⎿ <braille> [elapsed]     (updates every 100 ms)
//
// The label line is omitted when label is empty. When an estimate is provided
// the timer shows elapsed vs. estimated (e.g. "[8s / ~12s]"); once elapsed
// exceeds the estimate only elapsed is shown.
type Spinner struct {
	out      io.Writer
	estimate time.Duration
	label    string
	done     chan struct{}
	stopped  chan struct{}
}

// newSpinner creates and immediately starts a Spinner that writes to out.
// estimate is the predicted processing time (pass 0 when unavailable).
// label is the model identity string shown on the first line (e.g.
// "Ollama (phi3.5)" or "Ollama (phi3.5) · go-review").
// Call stop() when the work is done.
func newSpinner(out io.Writer, estimate time.Duration, label string) *Spinner {
	s := &Spinner{
		out:      out,
		estimate: estimate,
		label:    label,
		done:     make(chan struct{}),
		stopped:  make(chan struct{}),
	}
	go s.run()
	return s
}

// timerLabel formats the elapsed/estimate portion of the spinner line.
func (s *Spinner) timerLabel(elapsed time.Duration) string {
	e := elapsed.Round(time.Second)
	if s.estimate <= 0 || elapsed >= s.estimate {
		return "[" + e.String() + "]"
	}
	return "[" + e.String() + " / ~" + s.estimate.String() + "]"
}

// run is the background goroutine that animates the spinner.
//
// Layout (cursor position after each section's last write shown as █):
//
//	line 1 (label, optional): dim(label)\r\n
//	line 2 (message):         dim("  ⎿") coloredMsg\r\n
//	line 3 (spinner+timer):   dim("     ⎿") cyan(frame) dim(timer)█
//
// \r\n is used throughout instead of bare \n so the output is correct in
// both cooked mode (OPOST+ONLCR in effect) and raw mode (OPOST cleared).
//
// Fast updates (frameTick 100 ms): redraw line 3 only via \r…\033[K.
// Slow updates (msgTick 6 s):      move up to line 2 then redraw lines 2–3.
// Stop: move to top of block then erase to end of screen with \033[J.
func (s *Spinner) run() {
	defer close(s.stopped)

	start := time.Now()
	frameIdx := 0
	msgIdx := 0
	frameTick := time.NewTicker(100 * time.Millisecond)
	msgTick := time.NewTicker(6 * time.Second)
	defer frameTick.Stop()
	defer msgTick.Stop()

	colorMsg := func(idx int, msg string) string {
		return learColors[idx%len(learColors)](msg)
	}

	line2 := func(mi int) string {
		return dim("  ⎿") + " " + colorMsg(mi, LearMessages[mi])
	}
	line3 := func(fi int, elapsed time.Duration) string {
		return dim("     ⎿") + " " + cyan(spinnerFrames[fi]) + " " + dim(s.timerLabel(elapsed))
	}

	// upLines is how many lines above line 3 the spinner block begins.
	// Used by stop() to position the cursor for erasure.
	upLines := 1 // always line 2
	if s.label != "" {
		fmt.Fprintf(s.out, "%s\r\n", dim(s.label))
		upLines = 2
	}
	fmt.Fprintf(s.out, "%s\r\n", line2(msgIdx))
	fmt.Fprintf(s.out, "%s", line3(0, 0))
	// Cursor is now at the end of line 3 with no trailing newline.

	for {
		select {
		case <-s.done:
			// Move to the top of the spinner block, then erase to end of screen.
			fmt.Fprintf(s.out, "\033[%dA\r\033[J", upLines)
			return

		case <-msgTick.C:
			// Advance both the message and the frame index.
			msgIdx = (msgIdx + 1) % len(LearMessages)
			frameIdx = (frameIdx + 1) % len(spinnerFrames)
			elapsed := time.Since(start)
			// Move up to line 2, redraw it, then redraw line 3.
			fmt.Fprintf(s.out, "\033[1A\r%s\033[K\r\n%s\033[K",
				line2(msgIdx),
				line3(frameIdx, elapsed),
			)

		case <-frameTick.C:
			frameIdx = (frameIdx + 1) % len(spinnerFrames)
			elapsed := time.Since(start)
			// Redraw line 3 in place.
			fmt.Fprintf(s.out, "\r%s\033[K", line3(frameIdx, elapsed))
		}
	}
}

// stop halts the spinner animation and erases the spinner block from the
// terminal. It blocks until the background goroutine has exited.
func (s *Spinner) stop() {
	close(s.done)
	<-s.stopped
}
