package harvey

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"
)

/** parseLoopArgs parses the arguments passed to /loop after the command name.
 *
 * Parameters:
 *   args ([]string) — the arguments after "/loop", e.g. ["5m", "--count", "3", "hello", "world"]
 *
 * Returns:
 *   time.Duration — the parsed interval; always positive on success
 *   int           — number of iterations, in [1, 100]; default 10
 *   string        — the prompt or /command to repeat (tokens joined with spaces)
 *   error         — validation error (bad interval, bad count, or empty prompt)
 *
 * Example:
 *   interval, count, rest, err := parseLoopArgs([]string{"30s", "--count", "5", "check", "the", "build"})
 *   // interval=30s, count=5, rest="check the build", err=nil
 */
func parseLoopArgs(args []string) (time.Duration, int, string, error) {
	if len(args) == 0 {
		return 0, 0, "", fmt.Errorf("usage: /loop INTERVAL [--count N] PROMPT|/COMMAND")
	}

	interval, err := parseDurationString(args[0])
	if err != nil {
		return 0, 0, "", fmt.Errorf("loop: invalid interval %q — use e.g. 30s, 5m, 1h", args[0])
	}
	if interval <= 0 {
		return 0, 0, "", fmt.Errorf("loop: interval must be positive")
	}

	rest := args[1:]
	count := 10
	if len(rest) >= 2 && rest[0] == "--count" {
		n, parseErr := strconv.Atoi(rest[1])
		if parseErr != nil || n < 1 || n > 100 {
			return 0, 0, "", fmt.Errorf("loop: --count must be an integer between 1 and 100")
		}
		count = n
		rest = rest[2:]
	}

	prompt := strings.Join(rest, " ")
	if strings.TrimSpace(prompt) == "" {
		return 0, 0, "", fmt.Errorf("usage: /loop INTERVAL [--count N] PROMPT|/COMMAND")
	}

	return interval, count, prompt, nil
}

/** runLoopIteration dispatches a single /loop iteration — either a slash
 * command (when rest begins with "/") or a chat prompt via runChatTurn.
 *
 * Exit-sentinel commands (/exit, /quit, /bye) are recognised and return
 * exitRequested=true rather than being forwarded, so an accidental
 * "/loop 30s /exit" does not kill Harvey mid-loop.
 *
 * Parameters:
 *   ctx (context.Context) — the loop's shared cancellable context
 *   a (*Agent)            — the running agent
 *   rest (string)         — the prompt text or slash command to dispatch
 *   out (io.Writer)       — output destination
 *
 * Returns:
 *   bool  — true if an exit sentinel was encountered (loop should stop)
 *   error — any error from the dispatched command or chat turn
 *
 * Example:
 *   exit, err := runLoopIteration(ctx, a, "/git status", out)
 */
func runLoopIteration(ctx context.Context, a *Agent, rest string, out io.Writer) (bool, error) {
	if strings.HasPrefix(rest, "/") {
		name := strings.ToLower(strings.Fields(rest)[0][1:])
		if name == "exit" || name == "quit" || name == "bye" {
			return true, nil
		}
		_, err := a.dispatch(rest, out)
		return false, err
	}
	_, _, err := a.runChatTurn(ctx, rest, out, nil, false)
	return false, err
}

/** sleepInterruptible sleeps for d or until ctx is cancelled.
 *
 * Parameters:
 *   ctx (context.Context) — cancelled by the loop's SIGINT watcher
 *   d (time.Duration)     — how long to sleep when not cancelled
 *
 * Returns:
 *   bool — true if the sleep was interrupted by ctx cancellation
 *
 * Example:
 *   if sleepInterruptible(ctx, 5*time.Minute) {
 *       fmt.Fprintln(out, "Sleep interrupted.")
 *   }
 */
func sleepInterruptible(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return false
	case <-ctx.Done():
		return true
	}
}

/** cmdLoop is the handler for /loop. It repeats a prompt or slash command at
 * a fixed interval for a bounded number of iterations, blocking the REPL
 * until finished or cancelled. A single Ctrl+C cancels both the current
 * iteration and any pending sleep, then returns to the prompt.
 *
 * Parameters:
 *   a (Agent)       — the running agent
 *   args ([]string) — arguments after "/loop"; parsed by parseLoopArgs
 *   out (io.Writer) — output destination
 *
 * Returns:
 *   error — always nil; iteration errors are printed inline and do not
 *     stop the loop
 *
 * Example:
 *   // Equivalent to the user typing: /loop 5m --count 3 check the build
 *   cmdLoop(a, []string{"5m", "--count", "3", "check", "the", "build"}, os.Stdout)
 */
func cmdLoop(a *Agent, args []string, out io.Writer) error {
	interval, count, rest, err := parseLoopArgs(args)
	if err != nil {
		fmt.Fprintln(out, err.Error())
		return nil
	}

	fmt.Fprintf(out, dim("  Looping every %s, up to %d time(s): %s\n"), interval, count, rest)

	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	watchDone := make(chan struct{})
	go func() {
		defer signal.Stop(sigCh)
		select {
		case <-sigCh:
			cancel()
		case <-watchDone:
		}
	}()
	defer func() {
		close(watchDone)
		cancel()
	}()

	completed := 0
	cancelled := false
	stopped := false

	for i := 1; i <= count; i++ {
		if ctx.Err() != nil {
			cancelled = true
			break
		}
		fmt.Fprintf(out, dim("  [loop %d/%d]\n"), i, count)
		exitReq, iterErr := runLoopIteration(ctx, a, rest, out)
		if exitReq {
			fmt.Fprintf(out, dim("  loop: stopping — %q would exit Harvey\n"), rest)
			stopped = true
			break
		}
		if iterErr != nil {
			fmt.Fprintf(out, red("  loop error: ")+"%v\n", iterErr)
		}
		if ctx.Err() != nil {
			cancelled = true
			break
		}
		completed = i
		if i < count {
			if sleepInterruptible(ctx, interval) {
				cancelled = true
				break
			}
		}
	}

	switch {
	case cancelled:
		fmt.Fprintf(out, dim("  Loop cancelled after %d/%d iteration(s).\n"), completed, count)
	case stopped:
		fmt.Fprintf(out, dim("  Loop stopped after %d/%d iteration(s).\n"), completed, count)
	default:
		fmt.Fprintf(out, dim("  Loop finished after %d/%d iteration(s).\n"), completed, count)
	}
	return nil
}
