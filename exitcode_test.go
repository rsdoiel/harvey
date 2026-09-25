package harvey

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

// H1 of exit-codes-plan.md: the classifier. Every row of the workspace
// convention (workspace DR-0003) that the library can decide by itself.

func TestExitClass_CodesAreTheWorkspaceTable(t *testing.T) {
	for _, tc := range []struct {
		class ExitClass
		name  string
		code  int
	}{
		{ClassOK, "ok", 0}, {ClassNegative, "negative", 1}, {ClassUsage, "usage", 2},
		{ClassData, "data", 65}, {ClassNoInput, "no_input", 66},
		{ClassUnavailable, "unavailable", 69}, {ClassInternal, "internal", 70},
		{ClassCantCreate, "cant_create", 73}, {ClassIO, "io", 74},
		{ClassTempFail, "temp_fail", 75}, {ClassNoPermission, "no_permission", 77},
		{ClassConfig, "config", 78},
	} {
		if tc.class.Name != tc.name || tc.class.Code != tc.code {
			t.Errorf("class %q = {%q, %d}, want {%q, %d}", tc.name, tc.class.Name, tc.class.Code, tc.name, tc.code)
		}
	}
}

// refusedConn returns a real connection-refused error from a closed port.
func refusedConn(t *testing.T) error {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	addr := srv.URL
	srv.Close()
	_, err := http.Get(addr)
	if err == nil {
		t.Fatal("expected a connection error from a closed server")
	}
	return err
}

func TestExitCodeFor_ClassifiesStandardLibraryErrors(t *testing.T) {
	dir := t.TempDir()
	_, missing := os.Open(filepath.Join(dir, "nope"))
	_, notDir := os.ReadFile(dir) // reading a directory: a *PathError that is neither missing nor denied
	exists := os.Mkdir(dir, 0o755)
	var syn *json.SyntaxError
	jsonErr := json.Unmarshal([]byte("{"), &struct{}{})
	if !errors.As(jsonErr, &syn) && !errors.Is(jsonErr, io.ErrUnexpectedEOF) {
		t.Fatalf("test setup: %T is not a decode error", jsonErr)
	}

	for _, tc := range []struct {
		name string
		err  error
		want ExitClass
	}{
		{"nil", nil, ClassOK},
		{"file missing", missing, ClassNoInput},
		{"file missing, wrapped", fmt.Errorf("reading corpus: %w", missing), ClassNoInput},
		{"permission", &fs.PathError{Op: "open", Path: "/x", Err: fs.ErrPermission}, ClassNoPermission},
		{"already exists", exists, ClassCantCreate},
		{"other file error", notDir, ClassIO},
		{"connection refused", refusedConn(t), ClassUnavailable},
		{"url error", &url.Error{Op: "Get", URL: "http://x", Err: errors.New("boom")}, ClassUnavailable},
		{"net timeout", &net.DNSError{IsTimeout: true}, ClassUnavailable},
		{"context deadline", fmt.Errorf("chat: %w", context.DeadlineExceeded), ClassUnavailable},
		{"json decode", jsonErr, ClassData},
		{"unexpected EOF", io.ErrUnexpectedEOF, ClassData},
		{"library not found", fmt.Errorf("model %q: %w", "m", ErrNotFound), ClassNegative},
		{"library invalid", fmt.Errorf("bad value: %w", ErrInvalid), ClassUsage},
		{"unclassified plain error", errors.New("something nobody classified"), ClassInternal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExitCodeFor(tc.err); got != tc.want {
				t.Errorf("ExitCodeFor(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// An unclassified error is 70, never 1: a missing classification must show up
// as an exit code, not hide as an ordinary "no".
func TestExitCodeFor_UnclassifiedIsInternalNotNegative(t *testing.T) {
	got := ExitCodeFor(errors.New("plain"))
	if got.Code != 70 {
		t.Errorf("unclassified error exits %d, want 70", got.Code)
	}
	if _, classified := ExitClassOf(errors.New("plain")); classified {
		t.Error("ExitClassOf reports a plain error as classified")
	}
}

func TestClassedError_KeepsTheMessageAndTheCause(t *testing.T) {
	cause := fmt.Errorf("open x: %w", fs.ErrNotExist)
	err := ClassedAs(ClassData, cause)
	if err.Error() != cause.Error() {
		t.Errorf("message changed: %q vs %q", err.Error(), cause.Error())
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Error("errors.Is no longer reaches the cause")
	}
	if ClassedAs(ClassData, nil) != nil {
		t.Error("ClassedAs(nil) must stay nil")
	}
}

// An explicit class wins over what the wrapped error would classify as, and
// the outermost explicit class wins over an inner one.
func TestExitCodeFor_ExplicitClassWins(t *testing.T) {
	notExist := fmt.Errorf("x: %w", fs.ErrNotExist)
	if got := ExitCodeFor(ClassedAs(ClassData, notExist)); got != ClassData {
		t.Errorf("explicit data over not-exist = %v, want data", got)
	}
	inner := ClassedAs(ClassNoInput, errors.New("e"))
	if got := ExitCodeFor(ClassedAs(ClassConfig, inner)); got != ClassConfig {
		t.Errorf("outer config over inner no_input = %v, want config", got)
	}
	if got := ExitCodeFor(fmt.Errorf("context: %w", inner)); got != ClassNoInput {
		t.Errorf("wrapping with %%w lost the class: %v", got)
	}
}

func TestConstructors_ClassAndFormat(t *testing.T) {
	cause := errors.New("cause")
	for _, tc := range []struct {
		name string
		err  error
		want ExitClass
	}{
		{"Usagef", Usagef("bad %s", "x"), ClassUsage},
		{"Negativef", Negativef("bad %s", "x"), ClassNegative},
		{"NotFoundf", NotFoundf("bad %s", "x"), ClassNegative},
		{"Dataf", Dataf("bad %s", "x"), ClassData},
		{"NoInputf", NoInputf("bad %s", "x"), ClassNoInput},
		{"Unavailablef", Unavailablef("bad %s", "x"), ClassUnavailable},
		{"CantCreatef", CantCreatef("bad %s", "x"), ClassCantCreate},
		{"IOf", IOf("bad %s", "x"), ClassIO},
		{"Configf", Configf("bad %s", "x"), ClassConfig},
	} {
		if got := ExitCodeFor(tc.err); got != tc.want {
			t.Errorf("%s: class %v, want %v", tc.name, got, tc.want)
		}
		if tc.err.Error() != "bad x" {
			t.Errorf("%s: message %q, want %q", tc.name, tc.err.Error(), "bad x")
		}
	}
	// A constructor keeps %w so the cause stays reachable.
	if err := IOf("write: %w", cause); !errors.Is(err, cause) {
		t.Error("IOf does not keep the %w cause")
	}
}

func TestAsCreate_ReclassifiesFailuresToMakeAnOutput(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "afile")
	if err := os.WriteFile(f, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	underFile := os.MkdirAll(filepath.Join(f, "sub"), 0o755)
	_, parentGone := os.Create(filepath.Join(dir, "gone", "x"))
	denied := &fs.PathError{Op: "mkdir", Path: "/x", Err: fs.ErrPermission}
	explicit := Configf("bad")

	for _, tc := range []struct {
		name string
		err  error
		want ExitClass
	}{
		{"missing parent", parentGone, ClassCantCreate},
		{"path under a file", underFile, ClassCantCreate},
		{"permission keeps its own class", denied, ClassNoPermission},
		{"explicit class is kept", explicit, ClassConfig},
	} {
		if got := ExitCodeFor(AsCreate(tc.err)); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	if AsCreate(nil) != nil {
		t.Error("AsCreate(nil) must stay nil")
	}
}

// Real SQLite errors from the driver Harvey uses, not just result codes.
func TestExitCodeFor_SQLiteErrorsByResultCode(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO t (name) VALUES ('a')`); err != nil {
		t.Fatal(err)
	}
	_, unique := db.Exec(`INSERT INTO t (name) VALUES ('a')`)
	_, notNull := db.Exec(`INSERT INTO t (name) VALUES (NULL)`)
	if unique == nil || notNull == nil {
		t.Fatal("test setup: constraint violations did not fail")
	}
	if got := ExitCodeFor(unique); got != ClassData {
		t.Errorf("UNIQUE violation = %v, want data (DR-0049)", got)
	}
	if got := ExitCodeFor(notNull); got != ClassData {
		t.Errorf("NOT NULL violation = %v, want data", got)
	}

	// A file that is not a database.
	bad := filepath.Join(t.TempDir(), "bad.db")
	if err := os.WriteFile(bad, []byte("this is not a sqlite database, not even close........"), 0o644); err != nil {
		t.Fatal(err)
	}
	db2, _ := sql.Open("sqlite", bad)
	defer db2.Close()
	_, notADB := db2.Exec(`CREATE TABLE x (a)`)
	if notADB == nil {
		t.Fatal("test setup: a garbage file opened as a database")
	}
	if got := ExitCodeFor(notADB); got != ClassData {
		t.Errorf("not a database = %v, want data", got)
	}

	// Result codes that need no real database. Extended codes carry the primary
	// code in the low byte.
	for _, tc := range []struct {
		code int
		want ExitClass
	}{
		{5, ClassTempFail}, {6, ClassTempFail}, {517, ClassTempFail}, // BUSY, LOCKED, BUSY_SNAPSHOT
		{11, ClassData}, {26, ClassData}, {19, ClassData}, {2067, ClassData}, // CORRUPT, NOTADB, CONSTRAINT, UNIQUE
		{1, ClassIO}, {10, ClassIO}, {14, ClassIO}, // ERROR, IOERR, CANTOPEN
	} {
		if got := sqliteClass(tc.code); got != tc.want {
			t.Errorf("sqliteClass(%d) = %v, want %v", tc.code, got, tc.want)
		}
	}
}

// Wrapping keeps working for the classifier's own sentinels.
func TestSentinels_SurviveWrapping(t *testing.T) {
	err := fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", ErrNotFound))
	if !errors.Is(err, ErrNotFound) {
		t.Error("errors.Is lost ErrNotFound")
	}
}
