// Package harvey — permissions.go implements the workspace permissions system.
// This provides fine-grained control over which file operations are allowed
// in different parts of the workspace tree.
//
// Permissions are stored as a map from path prefix to list of allowed actions:
//   - read: Allow reading files/directories
//   - write: Allow creating/modifying files
//   - exec: Allow executing files as commands
//   - delete: Allow deleting files/directories
//
// Default: Workspace root ("") has all permissions. Subdirectories inherit
// from the closest parent with explicit permissions.
//
// Commands:
//   /permissions list [PATH]    — List permissions for PATH (default: all)
//   /permissions set PATH PERMS — Set permissions for PATH (e.g., /permissions set src/ read,write)
//   /permissions reset           — Reset to defaults (full access to root)
//
// The permissions are persisted in harvey.yaml under the permissions: key.

package harvey

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// ─── Command Handlers ────────────────────────────────────────────────────────

/** cmdPermissions handles workspace permission management.
 *
 * Subcommands:
 *   list [PATH]    — List permissions for a specific path or all paths
 *   set PATH PERMS — Set permissions for a path prefix (comma-separated: read,write,exec,delete)
 *   reset           — Reset all permissions to defaults
 *
 * Parameters:
 *   a    (*Agent)    — Harvey agent with permissions configuration.
 *   args ([]string)  — Command arguments from user input.
 *   out  (io.Writer) — Destination for command output.
 *
 * Returns:
 *   error — On command execution failure.
 *
 * Example:
 *   /permissions list
 *   /permissions list src/
 *   /permissions set src/ read,write
 *   /permissions reset
 */
func cmdPermissions(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(out, "Usage: /permissions <list [PATH]|set PATH PERMS|reset>")
		return nil
	}

	switch strings.ToLower(args[0]) {
	case "list":
		return permissionsList(a, args[1:], out)
	case "set":
		return permissionsSet(a, args[1:], out)
	case "reset":
		return permissionsReset(a, out)
	default:
		fmt.Fprintf(out, "Unknown permissions subcommand: %q\n", args[0])
		fmt.Fprintln(out, "Usage: /permissions <list [PATH]|set PATH PERMS|reset>")
	}
	return nil
}

func permissionsList(a *Agent, args []string, out io.Writer) error {
	if a.Config == nil || a.Config.Permissions == nil {
		fmt.Fprintln(out, "  No permissions configured.")
		return nil
	}

	// If a path is specified, show permissions for that path
	if len(args) > 0 {
		path := args[0]
		perms := a.Config.PermissionString(path)
		fmt.Fprintf(out, "  Permissions for %q: %s\n", path, perms)
		return nil
	}

	// Show all configured permissions
	if len(a.Config.Permissions) == 0 {
		fmt.Fprintln(out, "  No custom permissions configured (default: full access to root).")
		return nil
	}

	fmt.Fprintln(out, "  Configured permissions:")
	// Sort the prefixes for consistent output
	prefixes := make([]string, 0, len(a.Config.Permissions))
	for p := range a.Config.Permissions {
		prefixes = append(prefixes, p)
	}
	sort.Strings(prefixes)

	for _, prefix := range prefixes {
		perms := a.Config.PermissionString(prefix)
		fmt.Fprintf(out, "    %s: %s\n", prefix, perms)
	}
	return nil
}

func permissionsSet(a *Agent, args []string, out io.Writer) error {
	if len(args) < 2 {
		fmt.Fprintln(out, "Usage: /permissions set PATH PERMS")
		fmt.Fprintln(out, "  PERMS is a comma-separated list of: read, write, exec, delete")
		fmt.Fprintln(out, "  Example: /permissions set src/ read,write")
		return nil
	}

	path := args[0]
	permsStr := args[1]

	// Parse permissions
	perms := strings.Split(permsStr, ",")
	for i, p := range perms {
		perms[i] = strings.TrimSpace(p)
	}

	// Validate permissions
	validPerms := make(map[string]bool)
	for _, p := range AllPermissions {
		validPerms[p] = true
	}

	for _, p := range perms {
		if !validPerms[p] {
			fmt.Fprintf(out, "  Invalid permission: %q\n", p)
			fmt.Fprintf(out, "  Valid permissions: %s\n", strings.Join(AllPermissions, ", "))
			return nil
		}
	}

	// Ensure path ends with / for directory-like prefixes (except ".")
	if path != "." && !strings.HasSuffix(path, "/") {
		path = path + "/"
	}

	a.Config.SetPermission(path, perms)
	// Persist to harvey.yaml
	if a.Workspace != nil {
		if err := SaveRAGConfig(a.Workspace, a.Config); err != nil {
			fmt.Fprintf(out, "  Warning: could not save permissions: %v\n", err)
		}
	}
	fmt.Fprintf(out, "  Set permissions for %q: %s\n", path, strings.Join(perms, ", "))
	return nil
}

func permissionsReset(a *Agent, out io.Writer) error {
	a.Config.ResetPermissions()
	// Persist to harvey.yaml
	if a.Workspace != nil {
		if err := SaveRAGConfig(a.Workspace, a.Config); err != nil {
			fmt.Fprintf(out, "  Warning: could not save permissions: %v\n", err)
		}
	}
	fmt.Fprintln(out, "  Permissions reset to defaults.")
	fmt.Fprintln(out, "  Root (.) has: read, write, exec, delete")
	return nil
}

// ─── Permission Check Helpers ─────────────────────────────────────────────

// CheckReadPermission returns true if read is allowed for the given path.
func (a *Agent) CheckReadPermission(path string) bool {
	return a.HasPermission(path, PermRead)
}

// CheckWritePermission returns true if write is allowed for the given path.
func (a *Agent) CheckWritePermission(path string) bool {
	return a.HasPermission(path, PermWrite)
}

// CheckExecPermission returns true if exec is allowed for the given path.
func (a *Agent) CheckExecPermission(path string) bool {
	return a.HasPermission(path, PermExec)
}

// CheckDeletePermission returns true if delete is allowed for the given path.
func (a *Agent) CheckDeletePermission(path string) bool {
	return a.HasPermission(path, PermDelete)
}

// ─── Security Status Command ───────────────────────────────────────────────

/** cmdSecurity displays the current security settings status, including
 * safe mode, permissions, and audit buffer state. This provides a unified
 * view of all security-related configuration.
 *
 * Parameters:
 *   a    (*Agent)    — Harvey agent with security configuration.
 *   args ([]string)  — Command arguments (currently only "status" is supported).
 *   out  (io.Writer) — Destination for command output.
 *
 * Returns:
 *   error — On command execution failure.
 */
func cmdSecurity(a *Agent, args []string, out io.Writer) error {
	sep := strings.Repeat("=", 61)
	fmt.Fprintln(out, cyan(bold(sep)))
	fmt.Fprintln(out, bold("  Security Status"))
	fmt.Fprintln(out, cyan(bold(sep)))
	fmt.Fprintln(out)

	// Safe Mode
	fmt.Fprintln(out, bold("Safe Mode:"))
	if a.Config.SafeMode {
		fmt.Fprintln(out, "  Status: enabled")
		fmt.Fprintf(out, "  Allowed commands (%d): %s\n", len(a.Config.AllowedCommands), strings.Join(a.Config.AllowedCommands, ", "))
	} else {
		fmt.Fprintln(out, "  Status: disabled")
		fmt.Fprintln(out, "  All commands are allowed.")
	}
	fmt.Fprintln(out)

	// Permissions
	fmt.Fprintln(out, bold("Workspace Permissions:"))
	if a.Config.Permissions == nil || len(a.Config.Permissions) == 0 {
		fmt.Fprintln(out, "  No custom permissions configured.")
		fmt.Fprintln(out, "  Default: full access (read, write, exec, delete) to workspace root.")
	} else {
		fmt.Fprintln(out, "  Configured path permissions:")
		// Sort prefixes for consistent output
		prefixes := make([]string, 0, len(a.Config.Permissions))
		for p := range a.Config.Permissions {
			prefixes = append(prefixes, p)
		}
		sort.Strings(prefixes)
		for _, prefix := range prefixes {
			perms := a.Config.PermissionString(prefix)
			fmt.Fprintf(out, "    %s: %s\n", prefix, perms)
		}
	}
	fmt.Fprintln(out)

	// Audit Buffer
	fmt.Fprintln(out, bold("Audit Buffer:"))
	if a.AuditBuffer == nil {
		fmt.Fprintln(out, "  Not initialized.")
	} else {
		fmt.Fprintf(out, "  Events: %d/%d\n", a.AuditBuffer.Size(), a.AuditBuffer.Capacity())
	}
	fmt.Fprintln(out)

	fmt.Fprintln(out, dim("Attack surfaces controlled:"))
	fmt.Fprintln(out, dim("  • Command Execution: Safe Mode restricts which commands can run"))
	fmt.Fprintln(out, dim("  • File Access: Permissions control read/write/exec/delete per path"))
	fmt.Fprintln(out, dim("  • Environment Leakage: Filtered for all child processes"))
	fmt.Fprintln(out)

	fmt.Fprintln(out, cyan(bold(sep)))
	return nil
}
