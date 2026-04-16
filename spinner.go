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

// Spinner displays an animated spinner with whimsical Edward Lear messages
// while the LLM backend is processing a request. When an estimate is provided
// the timer shows elapsed vs. estimated (e.g. "[8s / ~12s]"); once elapsed
// exceeds the estimate only the elapsed time is shown.
type Spinner struct {
	out      io.Writer
	estimate time.Duration
	done     chan struct{}
	stopped  chan struct{}
}

// newSpinner creates and immediately starts a Spinner that writes to out.
// estimate is the predicted processing time from prior turn history; pass 0
// when no history is available. Call stop() when the work is done.
func newSpinner(out io.Writer, estimate time.Duration) *Spinner {
	s := &Spinner{
		out:      out,
		estimate: estimate,
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
func (s *Spinner) run() {
	defer close(s.stopped)

	start := time.Now()
	frameIdx := 0
	msgIdx := 0
	frameTick := time.NewTicker(100 * time.Millisecond)
	msgTick := time.NewTicker(6 * time.Second) // Slower rotation
	defer frameTick.Stop()
	defer msgTick.Stop()

	colorMsg := func(idx int, msg string) string {
		return learColors[idx%len(learColors)](msg)
	}

	// Draw initial frame immediately.
	fmt.Fprintf(s.out, "\r%s %s %s\033[K",
		cyan(spinnerFrames[0]),
		colorMsg(0, LearMessages[0]),
		dim(s.timerLabel(0)),
	)

	for {
		select {
		case <-s.done:
			// Erase the spinner line before returning.
			fmt.Fprint(s.out, "\r\033[K")
			return
		case <-msgTick.C:
			msgIdx = (msgIdx + 1) % len(LearMessages)
		case <-frameTick.C:
			frameIdx = (frameIdx + 1) % len(spinnerFrames)
			elapsed := time.Since(start)
			fmt.Fprintf(s.out, "\r%s %s %s\033[K",
				cyan(spinnerFrames[frameIdx]),
				colorMsg(msgIdx, LearMessages[msgIdx]),
				dim(s.timerLabel(elapsed)),
			)
		}
	}
}

// stop halts the spinner animation and erases the spinner line from the
// terminal. It blocks until the background goroutine has exited.
func (s *Spinner) stop() {
	close(s.done)
	<-s.stopped
}
