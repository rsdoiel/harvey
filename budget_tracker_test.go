package harvey

import "testing"

func TestBudgetTracker_ReserveFitsWithinTotal(t *testing.T) {
	b := NewBudgetTracker(100)
	if !b.Reserve(40) {
		t.Fatal("expected Reserve(40) to succeed against Total=100")
	}
	if b.Used != 40 {
		t.Errorf("Used = %d, want 40", b.Used)
	}
}

func TestBudgetTracker_ReserveRejectsWhenExceeded(t *testing.T) {
	b := NewBudgetTracker(100)
	if !b.Reserve(90) {
		t.Fatal("expected first Reserve(90) to succeed")
	}
	if b.Reserve(20) {
		t.Fatal("expected Reserve(20) to fail once Used+tokens (110) exceeds Total (100)")
	}
	if b.Used != 90 {
		t.Errorf("Used = %d, want 90 (rejected reservation must not be applied)", b.Used)
	}
}

func TestBudgetTracker_ReserveExactlyAtTotal(t *testing.T) {
	b := NewBudgetTracker(100)
	if !b.Reserve(100) {
		t.Fatal("expected Reserve(100) to succeed when it exactly fills Total")
	}
	if b.Remaining() != 0 {
		t.Errorf("Remaining() = %d, want 0", b.Remaining())
	}
}

func TestBudgetTracker_RemainingReflectsUsed(t *testing.T) {
	b := NewBudgetTracker(100)
	if got := b.Remaining(); got != 100 {
		t.Errorf("Remaining() before any Reserve = %d, want 100", got)
	}
	b.Reserve(30)
	if got := b.Remaining(); got != 70 {
		t.Errorf("Remaining() after Reserve(30) = %d, want 70", got)
	}
}
