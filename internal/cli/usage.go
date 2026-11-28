package cli

import (
	"fmt"
	"io"
)

const generalUsage = `Catcher command line

Usage:
  Catcher.exe <command> [options]

Commands:
  refresh   Refresh platforms and wait until finished
  list      List assignments
  status    Show login state, last refresh and counts (no network)
  mark      Set or clear the manual status of an assignment
  version   Print the program version
  help      Print this help

Options:
  --data-dir <path>   Data directory, defaults to the one used by the desktop app
  --help              Print usage for a command

Output:
  stdout carries JSON only, except for help; stderr carries diagnostics
  Exit codes: 0 success, 1 failure, 2 usage error, 3 login required

Examples:
  Catcher.exe refresh --platform chaoxing
  Catcher.exe list --show-submitted --keyword lab
  Catcher.exe mark 123 --status submitted

Agent integration:
  AGENTS.md in the install directory can be handed to a local-command agent

PowerShell does not wait for GUI programs; use cmd /c or Start-Process -Wait
`

const refreshUsage = `Usage:
  Catcher.exe refresh [--platform <p[,p]>] [--timeout <duration>] [--verbose] [--data-dir <path>]

Options:
  --platform   Platforms to refresh, comma separated or repeated; defaults to the enabled platforms
  --timeout    Total time limit, default 10m, 0 means unlimited
  --verbose    Also write logs to stderr

Notes:
  Platforms never logged in are marked skipped and do not count as failure;
  expired logins or network errors exit 1/3
`

const listUsage = `Usage:
  Catcher.exe list [--platform <p[,p]>] [--status <s[,s]>]
                   [--show-submitted] [--all-terms] [--keyword <text>] [--data-dir <path>]

Options:
  --platform        Limit to the given platforms, defaults to all
  --status          submitted, unsubmitted or unknown; matches the effective status
  --show-submitted  Include submitted assignments
  --all-terms       Do not limit to the current term
  --keyword         Match title or course name

Notes:
  Defaults to the current term (from September 1) and hides submitted assignments;
  assignments with unknown deadlines are always kept
`

const statusUsage = `Usage:
  Catcher.exe status [--data-dir <path>]

Reads the database and config only, no network requests
`

const markUsage = `Usage:
  Catcher.exe mark <id> --status <submitted|unsubmitted|clear> [--data-dir <path>]

Options:
  id        Assignment id from list or refresh output
  --status  submitted or unsubmitted sets the manual status; clear restores the platform value

Notes:
  Unknown or removed ids return NOT_FOUND
`

const versionUsage = `Usage:
  Catcher.exe version [--data-dir <path>]

Print the program version
`

// UsageError 把用法错误写到 stderr，退出 2
func UsageError(w io.Writer, message string) int {
	fmt.Fprintf(w, "usage error: %s\n\n%s", message, generalUsage)
	return ExitUsage
}

func printUsage(w io.Writer, text string) int {
	fmt.Fprint(w, text)
	return ExitOK
}
