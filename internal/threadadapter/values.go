package threadadapter

import (
	"encoding/json"
	"time"
)

type object = map[string]any

func obj(v any) object { m, _ := v.(map[string]any); return m }
func str(v any) string { s, _ := v.(string); return s }
func list(v any) []any { a, _ := v.([]any); return a }
func nullable(v any) any {
	if v == nil || v == "" {
		return nil
	}
	return v
}
func stamp(v any, ms bool) any {
	n, ok := v.(json.Number)
	if !ok {
		return nil
	}
	x, e := n.Int64()
	if e != nil || x < 0 {
		return nil
	}
	if ms {
		return time.UnixMilli(x).UTC()
	}
	return time.Unix(x, 0).UTC()
}
func number(v any) any {
	if n, ok := v.(json.Number); ok {
		return n
	}
	return nil
}
func providerError(v any) any {
	if v == nil {
		return nil
	}
	m := obj(v)
	s := str(v)
	if m != nil {
		s = str(m["message"])
	}
	if s == "" {
		return nil
	}
	return object{"code": nil, "message": s, "details": nil}
}

func iso(v any) any {
	if t, e := time.Parse(time.RFC3339Nano, str(v)); e == nil {
		return t.UTC()
	}
	return nil
}
func numeric(v any) float64 { n, _ := v.(json.Number); f, _ := n.Float64(); return f }
func strNumber(v any) string {
	n, ok := v.(json.Number)
	if !ok {
		return ""
	}
	return string(n)
}
