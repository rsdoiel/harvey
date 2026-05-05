// Package harvey — audit.go implements in-memory audit logging for security events.
// This provides a ring buffer that stores recent audit events without writing
// any files outside the workspace, addressing the requirement for no external file writes.
//
// Events are logged for:
//   - Command execution (via ! and /run)
//   - File operations (read, write, delete)
//   - Skill execution
//
// Commands:
//   /audit show [n]  — Show last n audit events (default: 10)
//   /audit clear     — Clear all audit events
//
// The buffer has a fixed capacity (default: 1000) and uses a ring buffer
// implementation for O(1) append and efficient memory usage.

package harvey

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// AuditAction represents the type of action being audited.
type AuditAction string

const (
	// Command-related actions
	ActionCommand AuditAction = "command"
	// File operation actions
	ActionFileRead    AuditAction = "file_read"
	ActionFileWrite   AuditAction = "file_write"
	ActionFileDelete  AuditAction = "file_delete"
	ActionFileList    AuditAction = "file_list"
	// Skill-related actions
	ActionSkillRun AuditAction = "skill_run"
	// Security-related actions
	ActionSecurity AuditAction = "security"
)

// AuditStatus represents the outcome of an audited action.
type AuditStatus string

const (
	StatusAllowed  AuditStatus = "allowed"
	StatusDenied   AuditStatus = "denied"
	StatusError    AuditStatus = "error"
	StatusSuccess  AuditStatus = "success"
	StatusWarning  AuditStatus = "warning"
)

// AuditEvent represents a single audit log entry.
type AuditEvent struct {
	Timestamp time.Time    // When the event occurred
	Action    AuditAction // Type of action (command, file_read, etc.)
	Details   string     // Description of what happened (command line, file path, etc.)
	Status    AuditStatus // Outcome (allowed, denied, error, etc.)
}

// Format returns a human-readable string representation of the event.
func (e AuditEvent) Format() string {
	return fmt.Sprintf("[%s] %s: %s (%s)",
		e.Timestamp.Format("15:04:05.000"),
		e.Action,
		e.Details,
		e.Status,
	)
}

// AuditBuffer is a thread-safe ring buffer for storing audit events.
type AuditBuffer struct {
	mu       sync.RWMutex
	events   []AuditEvent
	head     int  // next write position
	count    int  // number of events currently stored
	capacity int  // maximum capacity
}

// DefaultAuditBufferCapacity is the default size of the audit ring buffer.
const DefaultAuditBufferCapacity = 1000

// NewAuditBuffer creates a new AuditBuffer with the specified capacity.
func NewAuditBuffer(capacity int) *AuditBuffer {
	if capacity <= 0 {
		capacity = DefaultAuditBufferCapacity
	}
	return &AuditBuffer{
		events:   make([]AuditEvent, capacity),
		capacity: capacity,
	}
}

// Add appends an event to the buffer, overwriting the oldest event if full.
func (b *AuditBuffer) Add(event AuditEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.events[b.head] = event
	b.head = (b.head + 1) % b.capacity
	if b.count < b.capacity {
		b.count++
	}
}

// Log is a convenience method that creates and adds an event in one call.
func (b *AuditBuffer) Log(action AuditAction, details string, status AuditStatus) {
	b.Add(AuditEvent{
		Timestamp: time.Now(),
		Action:    action,
		Details:   details,
		Status:    status,
	})
}

// Get returns the last n events, most recent first.
func (b *AuditBuffer) Get(n int) []AuditEvent {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if n <= 0 || b.count == 0 {
		return nil
	}

	// Return at most n events, but no more than we have
	retCount := n
	if retCount > b.count {
		retCount = b.count
	}

	result := make([]AuditEvent, retCount)
	// Start from the most recent and go backwards
	start := b.head - 1
	if start < 0 {
		start = b.capacity - 1
	}

	for i := 0; i < retCount; i++ {
		idx := start - i
		if idx < 0 {
			idx += b.capacity
		}
		result[i] = b.events[idx]
	}

	return result
}

// GetAll returns all events in the buffer, most recent first.
func (b *AuditBuffer) GetAll() []AuditEvent {
	return b.Get(b.count)
}

// Clear removes all events from the buffer.
func (b *AuditBuffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.head = 0
	b.count = 0
	// Zero out the slice to help GC
	for i := range b.events {
		b.events[i] = AuditEvent{}
	}
}

// Size returns the current number of events in the buffer.
func (b *AuditBuffer) Size() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.count
}

// Capacity returns the maximum number of events the buffer can hold.
func (b *AuditBuffer) Capacity() int {
	return b.capacity
}

// ─── Commands ─────────────────────────────────────────────────────────────────

/** cmdAudit handles audit log management commands.
 *
 * Subcommands:
 *   show [n] — Show the last n audit events (default: 10)
 *   clear   — Clear all audit events
 *   status  — Show audit buffer status (count/capacity)
 *
 * Parameters:
 *   a    (*Agent)    — Harvey agent with audit buffer.
 *   args ([]string)  — Command arguments from user input.
 *   out  (io.Writer) — Destination for command output.
 *
 * Returns:
 *   error — On command execution failure.
 */
func cmdAudit(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(out, "Usage: /audit <show [n]|clear|status>")
		return nil
	}

	switch strings.ToLower(args[0]) {
	case "show":
		return auditShow(a, args[1:], out)
	case "clear":
		return auditClear(a, out)
	case "status":
		return auditStatus(a, out)
	default:
		fmt.Fprintf(out, "Unknown audit subcommand: %q\n", args[0])
		fmt.Fprintln(out, "Usage: /audit <show [n]|clear|status>")
	}
	return nil
}

func auditShow(a *Agent, args []string, out io.Writer) error {
	n := 10
	if len(args) > 0 {
		if _, err := fmt.Sscanf(args[0], "%d", &n); err != nil {
			fmt.Fprintf(out, "Invalid number: %s\n", args[0])
			return nil
		}
	}

	if a.AuditBuffer == nil {
		fmt.Fprintln(out, "  Audit buffer not initialized.")
		return nil
	}

	events := a.AuditBuffer.Get(n)
	if len(events) == 0 {
		fmt.Fprintln(out, "  No audit events recorded.")
		return nil
	}

	fmt.Fprintf(out, "  Last %d audit events:\n", len(events))
	for _, e := range events {
		fmt.Fprintf(out, "  %s\n", e.Format())
	}
	return nil
}

func auditClear(a *Agent, out io.Writer) error {
	if a.AuditBuffer == nil {
		fmt.Fprintln(out, "  Audit buffer not initialized.")
		return nil
	}
	a.AuditBuffer.Clear()
	fmt.Fprintln(out, "  Audit buffer cleared.")
	return nil
}

func auditStatus(a *Agent, out io.Writer) error {
	if a.AuditBuffer == nil {
		fmt.Fprintln(out, "  Audit buffer not initialized.")
		return nil
	}
	fmt.Fprintf(out, "  Audit buffer: %d/%d events\n", a.AuditBuffer.Size(), a.AuditBuffer.Capacity())
	return nil
}

// ─── Global Audit Helper ─────────────────────────────────────────────────────

// globalAuditBuffer is the package-level audit buffer used by AuditLog.
// atomic.Pointer provides safe concurrent access without a mutex.
var globalAuditBuffer atomic.Pointer[AuditBuffer]

// SetGlobalAuditBuffer sets the global audit buffer used by AuditLog.
func SetGlobalAuditBuffer(buf *AuditBuffer) {
	globalAuditBuffer.Store(buf)
}

// AuditLog logs an audit event to the global buffer if initialized.
func AuditLog(event AuditEvent) {
	if buf := globalAuditBuffer.Load(); buf != nil {
		buf.Add(event)
	}
}

// LogAudit is a convenience function for logging audit events.
func LogAudit(action AuditAction, details string, status AuditStatus) {
	AuditLog(AuditEvent{
		Timestamp: time.Now(),
		Action:    action,
		Details:   details,
		Status:    status,
	})
}
