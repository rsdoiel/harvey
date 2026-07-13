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
//
// Call UpdateStatus to show a transient message on line 2 in place of the Lear
// quote. The message reverts to a Lear quote at the next message tick (~6 s).
type Spinner struct {
	out      io.Writer
	estimate time.Duration
	label    string
	done     chan struct{}
	stopped  chan struct{}
	StatusCh chan string // receives transient status update strings
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
		StatusCh: make(chan string, 1),
	}
	go s.run()
	return s
}

/** UpdateStatus sends a transient status message to the spinner's message line.
 * Non-blocking: if a previous status has not been consumed, the new one
 * replaces it. Calling UpdateStatus on a stopped spinner is safe.
 *
 * Parameters:
 *   msg (string) — status string, e.g. "Calling read_file…"
 *
 * Example:
 *   spin.UpdateStatus("Searching knowledge base…")
 */
func (s *Spinner) UpdateStatus(msg string) {
	// Drain any pending status and replace with the latest.
	select {
	case <-s.StatusCh:
	default:
	}
	select {
	case s.StatusCh <- msg:
	default:
	}
}

/** ReportSensor renders a SensorEvent on the spinner's status line, using
 * its Message exactly as UpdateStatus would. Class/Kind are not yet
 * reflected in the rendering — pure routing plumbing per
 * harness-prerequisite-refactor-plan.md Phase C. See
 * harness-engineering-exploration.md Direction C for the planned sensor-
 * sidecar UI this is a prerequisite for, not the redesign itself.
 *
 * Parameters:
 *   ev (SensorEvent) — the sensor signal to display.
 *
 * Example:
 *   spin.ReportSensor(SensorEvent{Kind: "tool_call", Message: "Calling read_file…", Class: Computational})
 */
func (s *Spinner) ReportSensor(ev SensorEvent) {
	s.UpdateStatus(ev.Message)
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

	line2Lear := func(mi int) string {
		return dim("  ⎿") + " " + colorMsg(mi, LearMessages[mi])
	}
	line2Status := func(status string) string {
		return dim("  ⎿") + " " + dimGreen(status)
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
	fmt.Fprintf(s.out, "%s\r\n", line2Lear(msgIdx))
	fmt.Fprintf(s.out, "%s", line3(0, 0))
	// Cursor is now at the end of line 3 with no trailing newline.

	var lastStatus string     // most recently received status string
	var renderedStatus string // last status actually written to line 2

	for {
		select {
		case <-s.done:
			// Move to the top of the spinner block, then erase to end of screen.
			fmt.Fprintf(s.out, "\033[%dA\r\033[J", upLines)
			return

		case <-msgTick.C:
			// Lear rotation clears any pending status so quotes resume.
			lastStatus = ""
			renderedStatus = ""
			msgIdx = (msgIdx + 1) % len(LearMessages)
			frameIdx = (frameIdx + 1) % len(spinnerFrames)
			elapsed := time.Since(start)
			// Move up to line 2, redraw it, then redraw line 3.
			fmt.Fprintf(s.out, "\033[1A\r%s\033[K\r\n%s\033[K",
				line2Lear(msgIdx),
				line3(frameIdx, elapsed),
			)

		case <-frameTick.C:
			// Drain latest status.
			select {
			case s := <-s.StatusCh:
				lastStatus = s
			default:
			}

			frameIdx = (frameIdx + 1) % len(spinnerFrames)
			elapsed := time.Since(start)

			if lastStatus != "" && lastStatus != renderedStatus {
				// Status changed — redraw both line 2 and line 3.
				renderedStatus = lastStatus
				fmt.Fprintf(s.out, "\033[1A\r%s\033[K\r\n%s\033[K",
					line2Status(lastStatus),
					line3(frameIdx, elapsed),
				)
			} else {
				// Redraw line 3 only.
				fmt.Fprintf(s.out, "\r%s\033[K", line3(frameIdx, elapsed))
			}
		}
	}
}

// stop halts the spinner animation and erases the spinner block from the
// terminal. It blocks until the background goroutine has exited.
func (s *Spinner) stop() {
	close(s.done)
	<-s.stopped
}
