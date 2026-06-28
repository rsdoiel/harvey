# Harvey `/plan` IVR Support — Design Document

## Summary

This document describes the **design** for adding Instruct-Validate-Repair (IVR) pattern support to Harvey's `/plan` command. The IVR pattern addresses the fundamental reliability challenge with LLM-based agents: models produce incorrect or incomplete output 5-50% of the time (per [Mellea's research](https://mellea.ai/blogs/why-mellea/)).

**Status**: Deferred — design incomplete
**Author**: Generated with Mistral Vibe
**Date**: 2026-06-28
**Related Decision**: See DECISIONS.md (2026-06-28 — `/plan` IVR support deferred — design incomplete)

---

> **This design is deferred and incomplete.** The open questions in the section
> below are not cosmetic — they block implementation. Do not proceed with code
> until the validation-target question (what does each validation type validate
> against?) is resolved, the repair prompt token budget is specified, and the
> relationship with Idea 3 in [capability-adapter-concept.md](capability-adapter-concept.md)
> is explored. See DECISIONS.md for the full rationale.

---

## Problem Statement

Harvey's current `/plan` feature provides **bounded-context task execution**, which is a significant improvement over monolithic prompt approaches. Each step executes with only the system prompt and current plan state, keeping turn times constant regardless of plan length.

However, `/plan` lacks:

1. **Output validation**: No mechanism to verify that a step's output meets requirements
2. **Automatic repair**: No way to retry with corrective feedback when output is invalid
3. **Reliability metrics**: No visibility into step success rates or validation results

This means that even with bounded context, **steps can silently produce incorrect results**. Users must manually verify each step's output, which defeats the purpose of automation and contributes to the 80-95% failure rate observed in LLM pilot projects.

The IVR pattern directly addresses this by making validation a **first-class concern** in the execution flow.

---

## Requirements

### Functional Requirements

| ID | Requirement | Priority |
|----|-------------|----------|
| R1 | Support validation annotations on individual plan steps | Must Have |
| R2 | Validate step output after execution | Must Have |
| R3 | Provide automatic repair attempts with feedback when validation fails | Must Have |
| R4 | Surface validation results clearly in the UI | Must Have |
| R5 | Maintain backward compatibility with existing plans | Must Have |
| R6 | Preserve bounded-context execution model | Must Have |
| R7 | Support multiple validation types (command, file existence, regex, etc.) | Should Have |
| R8 | Make validation opt-in (not required for all steps) | Should Have |
| R9 | Configurable repair attempt limits | Could Have |
| R10 | Record IVR events in Fountain session format | Could Have |

### Non-Functional Requirements

| ID | Requirement | Priority |
|----|-------------|----------|
| NF1 | Minimal changes to existing `/plan` workflow | Must Have |
| NF2 | Deterministic validation (not LLM-based) | Must Have |
| NF3 | Fast validation execution (< 1 second for most types) | Should Have |
| NF4 | Clear error messages for validation failures | Must Have |
| NF5 | Local-first execution (no external dependencies) | Must Have |

---

## Constraints

1. **No changes to core `/plan` execution model**: Bounded context must be preserved
2. **No new top-level commands**: IVR extends `/plan`, not a separate feature
3. **No LLM-based validation**: Validation must be deterministic and reliable
4. **Backward compatible**: Existing plans must continue to work unchanged
5. **Safe mode compatible**: Must respect existing safe mode restrictions
6. **Workspace sandboxed**: All validation must operate within workspace constraints

---

## Architecture Overview

### Design Principle

**"Validation is code, not prompt."**

Following Mellea's philosophy, validation logic must be:
- **Explicit**: Defined by the user, not inferred by the LLM
- **Deterministic**: Same input always produces same validation result
- **Executable**: Runs in the host environment, not inside the LLM
- **Composable**: Multiple validations can be chained per step

### High-Level Components

```
┌─────────────────────────────────────────────────────────────────┐
│                           /plan next                               │
├─────────────────────────────────────────────────────────────────┤
│  1. Load Plan (with validation annotations)                      │
│  2. Execute Step (bounded context)                               │
│  3. Capture Output                                               │
│  4. Validation Phase ←───────────────────────────┐              │
│     ├─ Parse validations from step title           │              │
│     ├─ Run each validation against output          │              │
│     └─ Return pass/fail with error details         │              │
│  5. Decision: All passed? ──────────────────────────┼─ YES ──────► Mark done
│                                                   │              │
│                    NO                              │              │
│                                                   ▼              │
│  6. Repair Phase ←─────────────────────────────────────────────┘
│     ├─ Inject validation feedback into prompt
│     ├─ Re-execute step with fresh context
│     ├─ Re-validate output
│     └─ Repeat up to configured limit
│     
│  7. All repairs exhausted? ─────────────── YES ──────► Escalate to user
│                                              
│                                          NO ──────────► Continue repairs
└─────────────────────────────────────────────────────────────────┘
```

### Key Design Decisions

#### 1. Inline Annotation Syntax

**Decision**: Use GFM checklist format with inline annotations.

**Rationale**: 
- Consistent with existing `/plan` markdown format
- Human-readable and editable
- No new file format to learn
- Supports multiple validations per step

**Syntax**:
```markdown
- [ ] Step title [validate: type:value] [validate: type2:value2]
```

**Examples**:
- `- [ ] Build project [validate: command: go build ./...]`
- `- [ ] Create config file [validate: file_exists: config.yaml]`
- `- [ ] Write test [validate: regex: ^func Test]`
- `- [ ] Clean up [validate: file_not_exists: temp.txt]`

#### 2. Validation Types

**Decision**: Support a core set of validation types that cover 90% of use cases.

**Core Types**:
| Type | Purpose | Example |
|------|---------|---------|
| `command` | Run shell command, pass on exit 0 | `go build -o /dev/null file.go` |
| `file_exists` | File exists in workspace | `config.yaml` |
| `file_not_exists` | File does NOT exist | `temp.txt` |
| `regex` | Output matches regex pattern | `^func.*Test` |
| `contains` | Output contains string | `import "testing"` |
| `not_contains` | Output does NOT contain string | `TODO` |
| `no_errors` | No common error patterns detected | (no value) |
| `custom` | Run executable validation script | `./validate.sh` |

**Rationale**:
- Covers code generation, file operations, and output validation
- Simple to implement and understand
- Extensible (can add more types later)
- All types are deterministic and fast

#### 3. Opt-In Validation

**Decision**: Validation is opt-in via annotations; steps without annotations skip validation.

**Rationale**:
- Maintains backward compatibility
- Reduces friction for simple plans
- Users add validation only where needed
- Aligns with progressive enhancement principle

#### 4. Repair Mechanism

**Decision**: Automatic repair with configurable attempt limit (default: 2).

**Repair Flow**:
1. Validation fails with specific error message
2. Build repair prompt with:
   - Original plan context
   - Step description
   - Validation feedback (error message)
   - Previous output
3. Re-execute step with repair prompt
4. Re-validate
5. Repeat up to limit

**Rationale**:
- Matches how humans debug: try, get error, fix based on error
- Provides immediate feedback to the LLM
- Prevents infinite loops with attempt limit
- Configurable to balance automation vs. control

#### 5. Validation Feedback

**Decision**: Inject structured validation feedback into repair prompts.

**Feedback Format**:
```
VALIDATION FEEDBACK:
  Step failed with error: "<specific error message>"
  Validation type: <type>
  Expected: <what was expected>
  Actual: <what was received>

Please fix the issue and complete ONLY this step.
```

**Rationale**:
- Structured format is easier for LLMs to parse
- Specific error messages guide the repair
- Separates feedback from step instructions
- Prevents prompt injection

#### 6. Bounded Context Preservation

**Decision**: Validation and repair operate within the existing bounded context model.

**Implications**:
- Validation runs in host environment (Go), not LLM
- Repair attempts use same bounded context as original step
- Validation state is NOT added to conversation history
- Only the repair prompt includes validation feedback

**Rationale**:
- Preserves the performance benefits of bounded context
- Keeps validation separate from LLM execution
- Maintains consistency with existing `/plan` behavior

---

## Data Model

### Conceptual Types

#### ValidationRequirement
Represents a single validation rule for a plan step.

**Fields**:
- `Type`: Validation type (command, file_exists, regex, etc.)
- `Pattern`: For regex/contains validations, the pattern to match
- `Command`: For command/custom validations, the command to run
- `Negate`: Boolean flag for not_* validations

#### ValidationResult
Captures the outcome of a validation check.

**Fields**:
- `Requirement`: The validation that was run
- `Passed`: Boolean indicating success
- `Error`: Error message if failed
- `Duration`: How long validation took

#### Extended PlanStep
Adds IVR support to existing PlanStep.

**New Fields**:
- `Validations`: Slice of ValidationRequirement
- `RepairCount`: Number of repair attempts made
- `ValidationError`: Last validation error (if any)

**Note**: These fields are optional and only populated when IVR is used.

---

## Integration Points

### With Existing `/plan` Features

| Feature | Integration |
|---------|-------------|
| Bounded context | Validation runs in host; repair uses same bounded context as step |
| Model switching | Validation respects [model: name] annotations |
| Step tracking | RepairCount and ValidationError persisted in plan.md |
| Session recording | IVR events recorded as Fountain notes |

### With Harvey Infrastructure

| Component | Integration |
|-----------|-------------|
| Workspace sandboxing | All validation file operations use Workspace methods |
| Safe mode | Validation commands respect safe mode restrictions |
| Tool execution | Repair phase uses same tool execution path as regular steps |
| Configuration | IVR settings in agents/harvey.yaml under `plan:` stanza |

---

## User Experience Impact

### Before IVR
```
harvey > /plan next
  Executing step 1/3: Build project
  
  ` /run go build ./...`
  # github.com/user/project
  ./main.go:12: undefined: jwt
  
  ✓ Step 1/3 complete.
  
  << User must manually notice the error and fix it >>
```

### After IVR
```
harvey > /plan next
  Executing step 1/3: Build project [validate: command: go build -o /dev/null ./...]
  
  ` /run go build ./...`
  # github.com/user/project
  ./main.go:12: undefined: jwt
  
  Validating step 1...
    ✗ Validation failed: exit status 1
    Repair attempt 1/2...
    
    << LLM regenerates with validation feedback >>
    ` /run go get github.com/golang-jwt/jwt/v5`
    
    Validating step 1...
    ✓ All validations passed.
  
  ✓ Step 1/3 complete.
```

---

## Security Considerations

### Validation Command Execution
- Validation commands run with same environment filtering as `/run`
- Respect safe mode command allowlists
- Workspace sandboxing applies to all file operations
- Custom validation scripts must be executable and in workspace

### Preventing Validation Abuse
- Validation commands have timeout (default: 30s)
- Repair attempt limit prevents infinite loops
- Validation errors are sanitized before display
- No LLM-based validation prevents prompt injection

### Data Safety
- Validation operates on step output, not user data
- File existence checks use workspace-relative paths
- Custom scripts cannot access sensitive environment variables

---

## Performance Considerations

### Validation Overhead
| Validation Type | Expected Duration | Impact |
|----------------|-------------------|--------|
| file_exists | < 1ms | Negligible |
| file_not_exists | < 1ms | Negligible |
| contains | < 1ms | Negligible |
| not_contains | < 1ms | Negligible |
| regex | < 1ms | Negligible |
| no_errors | < 1ms | Negligible |
| command | Variable (command-dependent) | Configurable timeout |
| custom | Variable (script-dependent) | Configurable timeout |

**Mitigation**:
- Command and custom validations have configurable timeout
- Validation runs in parallel where possible (future optimization)
- Total validation time << LLM execution time for most workflows

### Repair Overhead
- Each repair attempt = 1 additional LLM call
- Default limit of 2 repairs = 2x LLM cost for failing steps
- Configurable limit allows users to balance cost vs. reliability

---

## Error Handling

### Validation Errors
| Error Type | Handling |
|------------|----------|
| Validation command fails | Return error message, attempt repair |
| Validation timeout | Fail validation, attempt repair |
| Invalid validation syntax | Skip validation, warn user, continue |
| Unknown validation type | Skip validation, warn user, continue |
| File not found (file_exists) | Fail validation, attempt repair |

### Repair Errors
| Error Type | Handling |
|------------|----------|
| LLM execution fails | Abort repair, escalate to user |
| All repair attempts exhausted | Escalate to user with final error |
| Repair validation fails | Continue to next repair attempt |

---

## Backward Compatibility

### Existing Plans
- Plans without validation annotations work exactly as before
- `Validations`, `RepairCount`, `ValidationError` fields are optional
- Serialization handles missing fields gracefully

### Existing Workflows
- `/plan TASK` unchanged
- `/plan next` unchanged for steps without validation
- `/plan status` extended to show validation info
- `/plan show` unchanged (shows raw markdown)
- `/plan clear` unchanged

---

## Open Questions

These questions must be answered before implementation begins. Questions 1–3
are **blocking** — they determine the core data model and execution path.

### Blocking

1. **What does each validation type validate against?**

   This is the most fundamental unresolved question. Two targets are possible:
   - *Model response text*: the raw text the LLM produced for this step
   - *Workspace state*: the filesystem after the step's tool calls completed

   `command` and `file_exists` clearly test workspace state. `regex` and
   `contains` are ambiguous: if the step writes a function to a file, does
   `[validate: regex:^func Test]` match the model's *reply text* (which
   might just say "Done") or does it read the *written file* (which has the
   function)? If workspace state, which file? This must be resolved per-type
   before the data model can be defined.

   A possible resolution: `regex`, `contains`, and `not_contains` always
   validate the model's response text; `file_exists`, `file_not_exists`, and
   `command` always validate workspace state. Separate `file_contains` and
   `file_regex` types can be added for file-content checks. This clean split
   makes the model unambiguous.

2. **What is the repair prompt token budget?**

   The current design injects "previous output" into the repair prompt.
   Previous output can be arbitrarily large (full file contents, build logs).
   On a 4K-context 8B model, this can exhaust the budget before the
   correction instruction is even included. A concrete cap — e.g., last
   500 tokens of previous output — must be specified and enforced.

3. **What is the `no_errors` validation type?**

   "No common error patterns detected" is not deterministic (NF2 violation).
   Either: define it precisely as "exit code 0 from the last `/run` command
   executed during the step" — making it redundant with `command: <cmd>` —
   or remove it from v1.

### Design questions (resolve before finalizing data model)

4. **Should multiple validations on one step run all-at-once or stop on first failure?**
   - Recommended: run all, collect all failures, include full list in repair prompt.

5. **Should validation annotations be stripped from step titles before the LLM sees them?**
   - Yes, required. The LLM must receive "Build project" not "Build project [validate: command: go build ./...]".
   - Verify `ParsePlanStep` is updated to strip annotations.

6. **Should validation failures block the entire plan?**
   - Recommended: yes, block at step level, matching CI behavior.

7. **Relationship with Idea 3 in capability-adapter-concept.md**:
   Idea 3 (strict output enforcement) applies the IVR pattern at the *format*
   level: Instruct (contract in system prompt) → Validate (schema check)
   → Repair (correction message). IVR here applies it at the *behavioral*
   level (did the step accomplish its goal?). Explore whether Idea 3's
   implementation reveals a general IVR execution primitive that `/plan`
   validation can reuse. This may resolve several of the ambiguities above.

---

## Success Criteria

This design is successful if:

1. Users can add validation to plan steps via simple annotations
2. Invalid output is automatically detected and repaired (within limits)
3. Validation failures are clearly communicated to users
4. Existing plans continue to work without modification
5. IVR adds < 100ms overhead for non-command validations
6. The feature is intuitive and discoverable

---

## Next Steps

After design approval:

1. **Create implementation plan** with detailed tasks and estimates
2. **Implement core validation types** (command, file_exists, regex, contains)
3. **Integrate with `/plan next`** command
4. **Add configuration support**
5. **Write tests**
6. **Document the feature**

---

## Appendix

### References

- [Mellea: Why Mellea?](https://mellea.ai/blogs/why-mellea/) — Original IVR pattern description
- [Mellea: Instruct-Validate-Repair](https://mellea.ai/blogs/getting-started-with-mellea/) — IVR implementation details
- [Harvey DECISIONS.md](../DECISIONS.md) — Related architectural decisions

### Related Harvey Files

- `plan.go` — Core plan data structures (will be extended)
- `plan_cmd.go` — Plan command handlers (will be modified)
- `config.go` — Configuration (will be extended)
- `recorder.go` — Session recording (may be extended)

### Glossary

| Term | Definition |
|------|------------|
| IVR | Instruct-Validate-Repair: A pattern for reliable LLM task execution |
| GFM | GitHub Flavored Markdown |
| LLM | Large Language Model |
