package mcp

import "fmt"

// IntArg reads an int-ish value from a tool's arguments map.
// JSON numbers arrive as float64, so we normalize all numeric forms.
func IntArg(args map[string]any, key string, def int) int {
	v, ok := args[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return def
}

// StringArg reads a string value from a tool's arguments map.
func StringArg(args map[string]any, key, def string) string {
	v, ok := args[key]
	if !ok {
		return def
	}
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return def
}

// ErrArg returns an error for tool argument validation failures.
func ErrArg(msg string) error { return fmt.Errorf("%s", msg) }
