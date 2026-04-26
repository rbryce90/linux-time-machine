package mcp

import (
	"reflect"
	"testing"
)

func TestIntArg(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		key  string
		def  int
		want int
	}{
		{"missing key returns default", map[string]any{}, "limit", 42, 42},
		{"int passes through", map[string]any{"limit": 7}, "limit", 0, 7},
		{"int64 narrowed", map[string]any{"limit": int64(99)}, "limit", 0, 99},
		// JSON unmarshal yields float64 for every number, even integers.
		// This case is the hot path in production.
		{"float64 from JSON narrowed", map[string]any{"limit": float64(15)}, "limit", 0, 15},
		{"unsupported type returns default", map[string]any{"limit": "20"}, "limit", 5, 5},
		{"nil value returns default", map[string]any{"limit": nil}, "limit", 8, 8},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IntArg(c.args, c.key, c.def); got != c.want {
				t.Errorf("IntArg(%v, %q, %d) = %d, want %d", c.args, c.key, c.def, got, c.want)
			}
		})
	}
}

func TestStringArg(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		key  string
		def  string
		want string
	}{
		{"missing key returns default", map[string]any{}, "metric", "cpu", "cpu"},
		{"string passes through", map[string]any{"metric": "mem"}, "metric", "cpu", "mem"},
		{"empty string falls back to default",
			map[string]any{"metric": ""}, "metric", "cpu", "cpu"},
		{"non-string falls back to default",
			map[string]any{"metric": 5}, "metric", "cpu", "cpu"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := StringArg(c.args, c.key, c.def); got != c.want {
				t.Errorf("StringArg(%v, %q, %q) = %q, want %q",
					c.args, c.key, c.def, got, c.want)
			}
		})
	}
}

func TestObjectSchema_RequiredOmittedWhenEmpty(t *testing.T) {
	props := map[string]any{"limit": IntProp("max")}

	noReq := ObjectSchema(props, nil)
	if _, ok := noReq["required"]; ok {
		t.Error("nil required slice should not produce a 'required' key")
	}
	emptyReq := ObjectSchema(props, []string{})
	if _, ok := emptyReq["required"]; ok {
		t.Error("empty required slice should not produce a 'required' key")
	}

	withReq := ObjectSchema(props, []string{"limit"})
	got, ok := withReq["required"].([]string)
	if !ok {
		t.Fatalf("required should be []string, got %T", withReq["required"])
	}
	if !reflect.DeepEqual(got, []string{"limit"}) {
		t.Errorf("required = %v, want [limit]", got)
	}

	if withReq["type"] != "object" {
		t.Errorf("type = %v, want object", withReq["type"])
	}
}

func TestPropertyHelpers(t *testing.T) {
	t.Run("IntProp", func(t *testing.T) {
		p := IntProp("how many")
		if p["type"] != "integer" {
			t.Errorf("type = %v, want integer", p["type"])
		}
		if p["description"] != "how many" {
			t.Errorf("description = %v, want 'how many'", p["description"])
		}
	})

	t.Run("StringProp", func(t *testing.T) {
		p := StringProp("a label")
		if p["type"] != "string" {
			t.Errorf("type = %v, want string", p["type"])
		}
		if p["description"] != "a label" {
			t.Errorf("description = %v, want 'a label'", p["description"])
		}
	})

	t.Run("StringEnumProp", func(t *testing.T) {
		p := StringEnumProp("metric", "cpu", "mem")
		if p["type"] != "string" {
			t.Errorf("type = %v, want string", p["type"])
		}
		got, ok := p["enum"].([]string)
		if !ok {
			t.Fatalf("enum should be []string, got %T", p["enum"])
		}
		if !reflect.DeepEqual(got, []string{"cpu", "mem"}) {
			t.Errorf("enum = %v, want [cpu mem]", got)
		}
	})
}

func TestErrArg(t *testing.T) {
	err := ErrArg("missing field")
	if err == nil || err.Error() != "missing field" {
		t.Errorf("ErrArg returned %v, want error with message 'missing field'", err)
	}
}
