// SPDX-License-Identifier: MIT

package format

import (
	"fmt"
	"maps"
	"strings"
)

// OutputFormat selects how CLI results are rendered to stdout.
type OutputFormat string

const (
	FormatText  OutputFormat = "text"
	FormatJSON  OutputFormat = "json"
	FormatTable OutputFormat = "table"
)

// Canonical stdout format strings (single source for parse, completion keys, tests).
var outputFormatValues = []string{string(FormatText), string(FormatJSON), string(FormatTable)}

// Short descriptions for shell completion (keys must match outputFormatValues).
var outputFormatDescriptions = map[string]string{
	string(FormatText):  "human-readable default",
	string(FormatJSON):  "machine-readable structured output",
	string(FormatTable): "human-readable tabular output",
}

// ValueDescriptions maps each legal stdout format string to a short completion hint.
// The returned map is a shallow copy; mutating it does not affect canonical definitions.
func ValueDescriptions() map[string]string {
	out := make(map[string]string, len(outputFormatDescriptions))
	maps.Copy(out, outputFormatDescriptions)
	return out
}

// Parse normalizes and validates a user-provided format string.
// Empty or whitespace-only strings default to [FormatText] (CLI default / unset env).
func Parse(s string) (OutputFormat, error) {
	f := OutputFormat(strings.ToLower(strings.TrimSpace(s)))
	if f == "" {
		return FormatText, nil
	}
	switch f {
	case FormatText, FormatJSON, FormatTable:
		return f, nil
	default:
		return "", fmt.Errorf("invalid format %q (expected: %s)", s, strings.Join(outputFormatValues, ", "))
	}
}
