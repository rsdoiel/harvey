%harvey(7) user manual | version 0.0.3 0969704
% R. S. Doiel
% 2026-05-12

# NAME

SECURITY — Safe Mode, workspace permissions, and audit logging

# SYNOPSIS

/safemode <on|off|status|allow CMD|deny CMD|reset>
/permissions <list [PATH]|set PATH PERMS|reset>
/audit <show [N]|clear|status>
/security status

# DESCRIPTION

Harvey includes four complementary security controls. All settings survive
restart when persisted via the commands below. Run /security status for a
unified view of the current security posture.

## SAFE MODE (/safemode)

Safe Mode restricts which programs the model may execute via the ! prefix
and /run. When enabled, only commands in the allowlist are permitted.

Default allowlist: ls, cat, grep, head, tail, wc, find, stat, jq, htmlq,
bat, batcat.

Subcommands:

  /safemode on
    Enable Safe Mode. Commands not in the allowlist are blocked and
    audit-logged.

  /safemode off
    Disable Safe Mode. All commands accepted by the shell metacharacter
    filter are permitted.

  /safemode status
    Show whether Safe Mode is on or off and list the current allowlist.

  /safemode allow CMD
    Add CMD to the allowlist. Persisted to agents/harvey.yaml.

  /safemode deny CMD
    Remove CMD from the allowlist. Persisted to agents/harvey.yaml.

  /safemode reset
    Restore the default allowlist.

## WORKSPACE PERMISSIONS (/permissions)

Workspace permissions give fine-grained read/write/exec/delete control per
path prefix within the workspace. Permissions are persisted in
agents/harvey.yaml under the permissions: key.

Permission values: read, write, exec, delete (comma-separated).

Subcommands:

  /permissions list [PATH]
    List permissions for all prefixes, or for a specific PATH.

  /permissions set PATH PERMS
    Set permissions for PATH. PERMS is a comma-separated list of values.
    Example: /permissions set src/ read,write

  /permissions reset
    Remove all custom permissions.

## AUDIT LOG (/audit)

Harvey maintains an in-memory ring buffer of the last 1 000 events covering
command execution, file reads, file writes, file deletes, file listings,
skill runs, and security denials. The log resets when Harvey exits.

Subcommands:

  /audit show [N]
    Print the most recent N events (default 20).

  /audit clear
    Clear the in-memory audit buffer.

  /audit status
    Show the buffer size and event count.

## SECURITY OVERVIEW (/security status)

/security status prints a single unified view of: Safe Mode state and
allowlist, workspace permissions, and audit buffer status.

# EXAMPLES

~~~
  harvey > /safemode on
  harvey > /safemode allow make
  harvey > /permissions set src/ read,write
  harvey > /audit show 10
  harvey > /security status
~~~

# SEE ALSO

  /help run      — shell command execution and timeout
  /help routing  — remote endpoint security

