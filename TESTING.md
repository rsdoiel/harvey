# Harvey Testing Guide

*Version 1.0 — Complete guide to testing Harvey*

## Overview

Harvey includes a comprehensive test suite to ensure reliability across its many features. This guide covers:

- **Running tests** — How to execute the test suite
- **Test architecture** — How tests are organized and structured
- **Testing strategies** — Approaches for different types of tests
- **Writing tests** — How to add new tests for Harvey features
- **Mocking** — How to test with mocked LLM backends
- **Integration testing** — Testing end-to-end workflows

## Quick Start

### Run All Tests

```bash
# From the harvey directory
make test

# Or directly with Go
cd harvey
go test ./...
```

### Run Tests for a Specific Package

```bash
# Test a specific file
go test -v -run TestWorkspace

# Test a specific package
go test -v ./... -run TestWorkspace

# Test with verbose output
go test -v ./...
```

### Test Coverage

```bash
# Show coverage for all packages
go test -cover ./...

# Show coverage with breakdown by function
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out

# Generate HTML coverage report
go tool cover -html=coverage.out -o coverage.html
```

## Test Architecture

### Test File Organization

Harvey follows Go's standard testing conventions with `_test.go` files alongside each source file:

```
harvey/
├── workspace.go          # Source file
├── workspace_test.go     # Tests for workspace.go
├── ollama_chat.go       # Source file
├── ollama_chat_test.go  # Tests for ollama_chat.go
├── recorder.go          # Source file
├── recorder_test.go     # Tests for recorder.go
├── commands.go          # Source file
├── commands_test.go     # Tests for commands.go
└── ...
```

### Test Categories

Harvey tests are organized into several categories based on the testing tier system:

| Category | Directory/Pattern | Purpose | Speed | Dependencies |
|----------|------------------|---------|-------|---------------|
| Unit | `*_test.go` | Test individual functions | Fast | None |
| Integration | `*_test.go` | Test component interactions | Medium | Minimal |
| Tier 1 | `tier1_test.go` | Core file operations | Medium | Workspace |
| Tier 2 | `tier2_test.go` | Code assistance features | Slow | Ollama |
| Tier 3 | `tier3_test.go` | Session quality features | Slow | Ollama |

### Test File Reference

| Test File | What It Tests | Key Tests |
|-----------|--------------|-----------|
| `workspace_test.go` | Workspace file operations | `TestWorkspaceNewWorkspace`, `TestWorkspaceAbsPath_valid`, `TestWorkspaceReadWriteFile` |
| `ollama_chat_test.go` | Ollama chat client (a fake /api/chat server) | `TestOllamaChat_writesContentAndMapsUsage`, `TestOllamaChat_toolCallInAChunk` |
| `recorder_test.go` | Session recording | `TestRecorderCreation`, `TestRecordTurn`, `TestFountainSyntax` |
| `knowledge_test.go` | Knowledge base operations | `TestOpenKnowledgeBase`, `TestProjectCRUD`, `TestObservationCRUD` |
| `routing_test.go` | @mention routing parsing | `TestParseAtMention_valid`, `TestParseAtMention_noMention` |
| `commands_test.go` | Command handlers | Various command-specific tests |
| `tier1_test.go` | File operations (read, write, search) | Tier 1 command tests |
| `tier2_test.go` | Code assistance (apply, run) | Tier 2 command tests |
| `tier3_test.go` | Session quality (clear, context) | Tier 3 command tests |

## Testing Strategies

### Unit Testing

Unit tests verify individual functions in isolation using Go's `testing` package.

**Example: Testing string manipulation**

```go
// In some_file_test.go
func TestExtractModelName(t *testing.T) {
    cases := []struct {
        input    string
        expected string
    }{
        {"Ollama (gemma4:latest)", "GEMMA4"},
        {"Ollama (MichelRosselli/apertus:latest)", "APERTUS"},
        {"anthropic (claude-sonnet-4-20250514)", "CLAUDE-SONNET-4-20250514"},
        {"none", "MODEL"},
    }
    
    for _, c := range cases {
        got := extractModelName(c.input)
        if got != c.expected {
            t.Errorf("extractModelName(%q) = %q, want %q", c.input, got, c.expected)
        }
    }
}
```

**Key characteristics:**
- Fast execution (< 1ms per test)
- No external dependencies
- Deterministic (same result every time)
- Test pure functions and logic

### Integration Testing

Integration tests verify that multiple components work together correctly.

**Example: Testing the chat client against a fake server**

`ollama_chat_test.go` starts an `httptest` server that speaks `/api/chat` and drives the real `OllamaChatClient` through it, so
the whole request and the streamed reply are exercised with no model.

### Mocking External Dependencies

Harvey uses **mock implementations** to test code that depends on external services (LLMs, embedders, etc.).

#### Mock LLM Client

```go
// From tier3_test.go (referenced in ARCHITECTURE.html)
type mockLLMClient struct {
    name    string
    responses []string
    callCount int
}

func (m *mockLLMClient) Name() string { return m.name }
func (m *mockLLMClient) Chat(ctx context.Context, history []ChatMessage, out io.Writer) (ChatStats, error) {
    if m.callCount >= len(m.responses) {
        return ChatStats{}, fmt.Errorf("no more mock responses")
    }
    response := m.responses[m.callCount]
    m.callCount++
    fmt.Fprint(out, response)
    return ChatStats{Model: m.name, Tokens: 10, Time: 0.1, TokensPerSec: 100}, nil
}
```

### Tiered Testing

Harvey uses a **tier system** for commands that have different testing requirements:

#### Tier 1: File Operations

Commands that perform basic file operations (read, write, search):
- `/read` — Read file into context
- `/search` — Search workspace
- `/git` — Git operations

**Testing approach:**
- Use real file system in temporary directories
- No LLM required
- Fast execution

**Example from tier1_test.go:**
```go
func TestReadCommand(t *testing.T) {
    // Setup test workspace
    ws, _ := NewWorkspace(t.TempDir())
    ws.WriteFile("test.txt", []byte("hello world"), 0o644)
    
    // Create agent
    a := NewAgent(ws, &Config{OllamaURL: "http://localhost:11434"})
    
    // Execute read command
    var buf bytes.Buffer
    err := cmdRead(a, []string{"test.txt"}, &buf)
    
    // Verify output
    if err != nil {
        t.Fatalf("cmdRead failed: %v", err)
    }
    if !strings.Contains(buf.String(), "hello world") {
        t.Errorf("expected output to contain 'hello world', got: %s", buf.String())
    }
}
```

#### Tier 2: Code Assistance

Commands that modify files or execute code:
- `/apply` — Apply tagged code blocks
- `/run` — Run shell commands
- `/edit` — Edit files

**Testing approach:**
- Use real file system
- Mock user confirmation for destructive operations
- May require Ollama for code generation

#### Tier 3: Session Quality

Commands that affect conversation state:
- `/clear` — Clear conversation history
- `/context` — Manage context
- `/kb` — Knowledge base operations

**Testing approach:**
- Full agent state setup
- Verify history modifications
- May require Ollama

## Running Tests

### Basic Test Commands

```bash
# Run all tests
go test ./...

# Run tests in a specific package
go test ./harvey

# Run a specific test
go test -v -run TestWorkspaceNewWorkspace

# Run tests matching a pattern
go test -v -run TestRag

# Run tests with race detector
go test -race ./...
```

### Test with Coverage

```bash
# Show coverage summary
go test -cover ./...

# Show coverage for each function
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out

# Generate HTML report
go tool cover -html=coverage.out -o coverage.html

# Open in browser (macOS)
open coverage.html

# Open in browser (Linux)
xdg-open coverage.html
```

### Test with Verbose Output

```bash
# Show test names as they run
go test -v ./...

# Show test names and timing
go test -v -test.v ./...

# Run a specific test with verbose output
go test -v -run TestIngestAndQuery
```

### Test with Timeout

```bash
# Set timeout for slow tests
go test -timeout 30s ./...

# Run with short timeout for quick feedback
go test -timeout 5s -run TestWorkspace
```

### Run Tests in Parallel

```bash
# Run tests in parallel (where supported)
go test -parallel 4 ./...
```

### Clean Test Artifacts

```bash
# Remove test databases
go clean -testcache

# Remove temporary files
rm -f test_*.db
rm -rf testout/
```

## Writing Tests

### Test Structure Template

```go
package harvey

import (
    "testing"
)

// TestXxx tests [description of what's being tested]
func TestXxx(t *testing.T) {
    // Setup test data
    // 
    // Execute code under test
    // 
    // Verify results
}

// TestXxxError tests error conditions for Xxx
func TestXxxError(t *testing.T) {
    // Test error paths
}

// TestXxxEdgeCases tests edge cases for Xxx
func TestXxxEdgeCases(t *testing.T) {
    // Test boundary conditions
    // Test empty inputs
    // Test invalid inputs
}
```

### Table-Driven Tests

Harvey extensively uses **table-driven tests** for comprehensive coverage:

```go
func TestExtractModelName(t *testing.T) {
    cases := []struct {
        name     string // Test case name
        input    string // Input to function
        expected string // Expected output
    }{
        {
            name:     "Ollama gemma4",
            input:    "Ollama (gemma4:latest)",
            expected: "GEMMA4",
        },
        {
            name:     "Ollama with namespace",
            input:    "Ollama (MichelRosselli/apertus:latest)",
            expected: "APERTUS",
        },
        {
            name:     "Anthropic Claude",
            input:    "anthropic (claude-sonnet-4-20250514)",
            expected: "CLAUDE-SONNET-4-20250514",
        },
        {
            name:     "No model",
            input:    "none",
            expected: "MODEL",
        },
    }
    
    for _, c := range cases {
        t.Run(c.name, func(t *testing.T) {
            got := extractModelName(c.input)
            if got != c.expected {
                t.Errorf("extractModelName(%q) = %q, want %q", c.input, got, c.expected)
            }
        })
    }
}
```

**Benefits of table-driven tests:**
- Easy to add new test cases
- Clear separation of test data from test logic
- Sub-tests are reported individually
- Parallelizable by default

### Testing File Operations

```go
func TestWorkspaceOperations(t *testing.T) {
    // Create temporary workspace
    ws, err := NewWorkspace(t.TempDir())
    if err != nil {
        t.Fatal(err)
    }
    
    // Test file write
    content := []byte("test content")
    err = ws.WriteFile("test.txt", content, 0o644)
    if err != nil {
        t.Fatalf("WriteFile failed: %v", err)
    }
    
    // Test file read
    got, err := ws.ReadFile("test.txt")
    if err != nil {
        t.Fatalf("ReadFile failed: %v", err)
    }
    if string(got) != string(content) {
        t.Errorf("ReadFile returned wrong content")
    }
    
    // Test escape prevention
    _, err = ws.AbsPath("../../etc/passwd")
    if err == nil {
        t.Error("Expected error for escape path")
    }
}
```

### Testing with Temporary Directories

```go
func TestWithTempDir(t *testing.T) {
    // Create a temporary directory that's automatically cleaned up
    tempDir := t.TempDir()
    
    // Use the temp dir for your test
    ws, err := NewWorkspace(tempDir)
    if err != nil {
        t.Fatal(err)
    }
    
    // Files created in tempDir will be automatically removed
    // when the test completes
}
```

## Testing Specific Features

### Testing Knowledge Base

Knowledge base tests verify SQLite operations:

```bash
# Run knowledge base tests
go test -v -run TestKnowledge

# Test specific operations
go test -v -run TestKnowledgeBase_Projects
go test -v -run TestKnowledgeBase_AddProject
```

### Testing Session Recording

Session recording tests verify Fountain file generation:

```bash
# Run recorder tests
go test -v -run TestRecorder

# Test Fountain syntax output
go test -v -run TestFountainSrc
```

### Testing Workspace Operations

Workspace tests verify file sandboxing and operations:

```bash
# Run workspace tests
go test -v -run TestWorkspace

# Test escape prevention
go test -v -run TestWorkspace.*escape
```

## Continuous Integration

### GitHub Actions Workflow

Harvey uses GitHub Actions for CI/CD. The workflow (in `.github/workflows/`) typically includes:

```yaml
name: Go

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      
      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.26'
      
      - name: Run tests
        run: go test ./...
      
      - name: Check coverage
        run: |
          go test -coverprofile=coverage.out ./...
          go tool cover -func=coverage.out
```

### Running CI Locally

To simulate CI environment locally:

```bash
# Run all tests (same as CI)
go test ./...

# Run with race detector (CI typically does this)
go test -race ./...

# Check that build works
go build ./cmd/harvey
```

## Test Data and Fixtures

### Test Data Generation

Harvey tests generate test data dynamically rather than using pre-made fixtures:

```go
// Generate test documents
func generateTestDocs() []string {
    return []string{
        "This is a test document about Harvey",
        "Harvey is a terminal-based coding agent",
        "It uses Ollama for local LLM access",
    }
}
```

### Temporary Files

All test files should use `t.TempDir()` or explicit cleanup:

```go
func TestWithFiles(t *testing.T) {
    // Good: Uses temp dir
    tempDir := t.TempDir()
    path := filepath.Join(tempDir, "test.txt")
    
    // Good: Explicit cleanup
    dbPath := "test.db"
    defer os.Remove(dbPath)
    
    // Bad: Leaves files behind
    // _ = os.Create("test-file.txt")
}
```

## Debugging Tests

### Verbose Output

```bash
# Show detailed test output
go test -v -run TestName

# Show even more detail with test.v flag
go test -v -test.v -run TestName
```

### Running Individual Tests

```bash
# Run a single test
go test -v -run TestName
# Run tests matching a pattern
go test -v -run TestWorkspace

# Run only sub-tests matching a pattern
go test -v -run TestWorkspace/AbsPath
```

### Using delve for Debugging

```bash
# Install delve
go install github.com/go-delve/delve/cmd/dlv@latest

# Debug a test
dlv test -run TestName

# Set breakpoints
b harvey.go:123

# Continue execution
c

# Print variables
p variableName
```

### Adding Debug Output

For temporary debug output in tests:

```go
func TestDebug(t *testing.T) {
    // Use t.Log for debug output that only shows on failure
    t.Log("Debug value:", someValue)
    
    // Use t.Logf for formatted debug output
    t.Logf("Processing %d items", len(items))
    
    // Use fmt.Println for always-visible output (not recommended in tests)
    // fmt.Println("DEBUG:", value) // Don't commit this
}
```

## Performance Testing

### Benchmark Tests

Harvey includes benchmark tests for performance-critical code:

```go
func BenchmarkCosineSimilarity(b *testing.B) {
    vec1 := make([]float64, 768) // Typical embedding dimension
    vec2 := make([]float64, 768)
    for i := range vec1 {
        vec1[i] = float64(i)
        vec2[i] = float64(i * 2)
    }
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        cosineSimilarity(vec1, vec2)
    }
}
```

**Running benchmarks:**
```bash
# Run all benchmarks
go test -bench .

# Run specific benchmark
go test -bench BenchmarkCosineSimilarity

# Run with memory profiling
go test -bench . -benchmem

# Run with profiling
go test -bench . -cpuprofile=cpu.out -memprofile=mem.out
```

### Analyzing Benchmark Results

```bash
# Compare before/after changes
go test -bench . -benchtime=1s

# Generate profiling reports
go tool pprof cpu.out
```

## Test Coverage Analysis

### Identifying Uncovered Code

```bash
# Generate coverage profile
go test -coverprofile=coverage.out ./...

# Show uncovered lines
go tool cover -func=coverage.out | grep -v "100.0%"

# Show coverage for a specific file
go test -coverprofile=coverage.out -run TestXxx
go tool cover -func=coverage.out | grep "file.go"
```

### Improving Coverage

To add tests for uncovered code:

1. Identify uncovered functions from coverage report
2. Add table-driven tests for each function
3. Test error paths and edge cases
4. Verify coverage improved: `go test -cover .`

## Pre-commit Testing

### Before Committing

```bash
# Run all tests
go test ./...

# Run with race detector
go test -race ./...

# Check coverage
go test -cover ./...

# Build to check for compile errors
go build ./cmd/harvey
```

### Git Hooks

Add a pre-commit hook to run tests automatically:

```bash
# .git/hooks/pre-commit (make executable)
#!/bin/sh

echo "Running tests..."
go test ./...
if [ $? -ne 0 ]; then
    echo "Tests failed, commit aborted"
    exit 1
fi

echo "Running race detector..."
go test -race ./...
if [ $? -ne 0 ]; then
    echo "Race detector found issues, commit aborted"
    exit 1
fi

exit 0
```

## Troubleshooting Tests

### Common Test Failures

| Failure | Likely Cause | Solution |
|---------|--------------|----------|
| `database is locked` | SQLite WAL mode contention | Use `MaxOpenConns(1)` or sequential tests |
| `file already exists` | Test didn't clean up | Use `t.TempDir()` or explicit cleanup |
| `model mismatch` | Wrong mock embedder name | Ensure mock embedder name matches store |
| `permission denied` | File permissions | Check temp dir permissions |
| `context deadline exceeded` | Slow test or timeout | Increase timeout or optimize test |
| `no such table` | Database not initialized | Run schema migration first |

### SQLite Locking Issues

SQLite WAL mode can cause locking issues in parallel tests:

```bash
# Run tests sequentially
go test -p 1 ./...

# Or fix the code to handle concurrent access
```

Harvey uses `PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL;` and `MaxOpenConns(1)` to prevent locking issues.

### Test Dependencies

Some tests require external dependencies:

| Dependency | Required For | Setup |
|------------|--------------|-------|
| Ollama | Tier 2/3 tests, RAG tests | `ollama serve` |
| Embedding models | RAG tests | `ollama pull nomic-embed-text` |
| SQLite | All tests | Included in Go SQLite driver |

**Skip tests that require dependencies:**

```go
func TestWithOllama(t *testing.T) {
    if os.Getenv("OLLAMA_URL") == "" {
        t.Skip("OLLAMA_URL not set, skipping Ollama-dependent test")
    }
    // Test code
}
```

## Contributing Tests

### Adding New Tests

1. **Create a new test file** alongside the source file (e.g., `new_feature_test.go`)
2. **Follow existing patterns** from similar test files
3. **Use table-driven tests** for comprehensive coverage
4. **Test edge cases** and error conditions
5. **Keep tests fast** (< 100ms each where possible)
6. **Clean up after tests** (use `t.TempDir()` or `defer`)

### Example: Adding Tests for a New Function

```go
// In source file (harvey.go)
func NewFeature(input string) (string, error) {
    if input == "" {
        return "", errors.New("empty input")
    }
    return strings.ToUpper(input), nil
}

// In test file (harvey_test.go)
func TestNewFeature(t *testing.T) {
    cases := []struct {
        name     string
        input    string
        want    string
        wantErr bool
    }{
        {
            name:  "valid input",
            input: "hello",
            want:  "HELLO",
        },
        {
            name:   "empty input",
            input:  "",
            wantErr: true,
        },
    }
    
    for _, c := range cases {
        t.Run(c.name, func(t *testing.T) {
            got, err := NewFeature(c.input)
            if (err != nil) != c.wantErr {
                t.Errorf("NewFeature() error = %v, wantErr %v", err, c.wantErr)
            }
            if !c.wantErr && got != c.want {
                t.Errorf("NewFeature() = %v, want %v", got, c.want)
            }
        })
    }
}
```

### Test Review Checklist

Before submitting a PR with new tests:

- [ ] All new code has corresponding tests
- [ ] Tests follow existing patterns and conventions
- [ ] Tests pass locally
- [ ] Tests pass with `-race` flag
- [ ] Coverage is maintained or improved
- [ ] No test files left in repository (use `t.TempDir()`)
- [ ] Tests are reasonably fast (< 1 second each)
- [ ] Edge cases and error conditions are tested

## Resources

### Go Testing Documentation

- [Go Testing Package](https://pkg.go.dev/testing) — Official Go testing documentation
- [Go Testing Blog Post](https://go.dev/blog/testing) — Go blog on testing
- [Effective Go: Testing](https://go.dev/doc/effective_go#testing) — Testing section of Effective Go
- [Go Test Examples](https://pkg.go.dev/testing#examples) — Example tests

### Harvey-Specific Resources

- [ARCHITECTURE.html](ARCHITECTURE.html) — Technical architecture and test coverage
- [CONFIGURATION.md](CONFIGURATION.md) — Configuration for test environments
- [ollama_chat_test.go](ollama_chat_test.go) — Example of testing against a fake server
- [workspace_test.go](workspace_test.go) — Example of workspace tests

## See Also

- [Makefile](Makefile) — Build and test commands
- [CONTRIBUTING.md](CONTRIBUTING.md) — Contribution guidelines (if exists)
- [CONFIGURATION.md](CONFIGURATION.md) — Configuration reference
- [getting_started.md](getting_started.md) — Getting started guide
- [ARCHITECTURE.html](ARCHITECTURE.html) — Technical architecture

*Documentation generated from test files and Makefile. Version 1.0.*
