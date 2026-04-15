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

// learMessages are whimsical waiting messages in the style of Edward Lear's
// Book of Nonsense, displayed while the LLM is thinking.
var learMessages = []string{
	"The Pobble who has no toes is pondering your query...",
	"The Owl and the Pussycat sail by the light of thought...",
	"The Jumblies have gone to sea in a sieve to fetch your answer...",
	"The Dong with the luminous nose searches through the dark...",
	"The Quangle Wangle's hat is collecting scattered ideas...",
	"The Yonghy-Bonghy-Bò contemplates upon the coast...",
	"The Nutcrackers and the Sugar-Tongs are in conference...",
	"The runcible spoon stirs the pot of possibilities...",
	"Far and few, far and few, the thoughts are gathering...",
	"The Bong-tree sways as your answer takes its shape...",
	"The Pobble's Aunt Jobiska sets the kettle to boil...",
	"The pelican, the stork, and the crane are consulting...",
	"Old Foss the cat regards the question with one eye...",
	"The Table and the Chair have set off on a journey...",
	"The calico pie is being examined most carefully...",
	"The Scroobious Pip sits still and waits with great patience...",
	"The two old Bachelors are brewing something exceedingly odd...",
	"The Pobble has wrapped his nose in crimson flannel to think...",
	"The Quangle Wangle sighs a sigh of singular serenity...",
	"The Cummerbund glides through the twilight seeking answers...",
	"The Blue and Spotted Frog deliberates upon the matter...",
	"The Biscuit-Tree rustles its leaves with whispering wisdom...",
	"The Dolomphious Duck considers the question from all angles...",
	"The Fimble Fowl fans the air in frantic meditation...",
	"The Absolutely Abstemious Ass awaits the answer patiently...",
	"The Enthusiastic Elephant encourages the thinking along...",
	"The Luminous Nose illuminates the dark corridors of thought...",
	"The Pelican chorus rehearses your answer in harmonious verse...",
	"The Pobble swam across the Bristol Channel to find this...",
	"On the coast of Coromandel the answer dances by the sea...",
	"The Quangle Wangle waves a welcome to wandering words...",
	"The Dong has lit his luminous nose and gone in search...",
	"The Jumblies have sailed far and few to gather your reply...",
	"The Runcible Cat observes the proceedings with great dignity...",
	"The Pobblian philosophers of the far Gromboolian plain confer...",
	"Somewhere beyond the Chankly Bore the answer stirs...",
	"The Hills of the Chankly Bore echo with distant deliberation...",
	"The Nutcrackers have cracked open a particularly tricky notion...",
	"The Sugar-Tongs have seized upon a promising thread of thought...",
	"The Scroobious Snake coils thoughtfully around your question...",
	"The Quangle Wangle's hat has attracted seventeen new ideas...",
	"In the land where the Bong-tree grows the answer ripens...",
	"The Pobble's toes may be gone but his wits are very much present...",
	"The Owl has consulted the elegant fowl and the piggy-wig too...",
	"They dined on mince and slices of quince while pondering this...",
	"The pea-green boat rocks gently as the answer is composed...",
	"Old Foss has knocked three times upon the pantry door...",
	"The Yonghy-Bonghy-Bò has written the question in the sand...",
	"The Lady Jingly answers from across the Coromandel shore...",
	"The Plum-pudding Flea is hopping toward a conclusion...",
}

/** Spinner displays an animated spinner with whimsical Edward Lear messages
 * while the LLM backend is processing a request. When an estimate is provided
 * the timer shows elapsed vs. estimated (e.g. "[8s / ~12s]"); once elapsed
 * exceeds the estimate only the elapsed time is shown.
 *
 * Example:
 *   sp := newSpinner(os.Stdout, 0)
 *   // ... do slow work ...
 *   sp.stop()
 */
type Spinner struct {
	out      io.Writer
	estimate time.Duration
	done     chan struct{}
	stopped  chan struct{}
}

/** newSpinner creates and immediately starts a Spinner that writes to out.
 * estimate is the predicted processing time from prior turn history; pass 0
 * when no history is available. Call stop() when the work is done.
 *
 * Parameters:
 *   out      (io.Writer)     — destination for spinner animation output.
 *   estimate (time.Duration) — predicted duration; 0 disables the estimate display.
 *
 * Returns:
 *   *Spinner — running spinner; caller must call stop().
 *
 * Example:
 *   sp := newSpinner(os.Stdout, agent.estimateDuration())
 *   defer sp.stop()
 */
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
	msgTick := time.NewTicker(4 * time.Second)
	defer frameTick.Stop()
	defer msgTick.Stop()

	colorMsg := func(idx int, msg string) string {
		return learColors[idx%len(learColors)](msg)
	}

	// Draw initial frame immediately.
	fmt.Fprintf(s.out, "\r%s %s %s", cyan(spinnerFrames[0]), colorMsg(0, learMessages[0]), dim(s.timerLabel(0)))

	for {
		select {
		case <-s.done:
			// Erase the spinner line before returning.
			fmt.Fprint(s.out, "\r\033[K")
			return
		case <-msgTick.C:
			msgIdx = (msgIdx + 1) % len(learMessages)
		case <-frameTick.C:
			frameIdx = (frameIdx + 1) % len(spinnerFrames)
			elapsed := time.Since(start)
			fmt.Fprintf(s.out, "\r%s %s %s",
				cyan(spinnerFrames[frameIdx]),
				colorMsg(msgIdx, learMessages[msgIdx]),
				dim(s.timerLabel(elapsed)),
			)
		}
	}
}

/** stop halts the spinner animation and erases the spinner line from the
 * terminal. It blocks until the background goroutine has exited.
 *
 * Example:
 *   sp := newSpinner(os.Stdout)
 *   // ... do slow work ...
 *   sp.stop()
 */
func (s *Spinner) stop() {
	close(s.done)
	<-s.stopped
}
