package tasks

import (
	_ "embed"
	"encoding/json"
	"slices"
)

type PropertyOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

//go:embed properties.json
var propertyJSON []byte
var properties = func() map[string][]PropertyOption {
	var p map[string][]PropertyOption
	if err := json.Unmarshal(propertyJSON, &p); err != nil {
		panic(err)
	}
	return p
}()

func PropertyOptions(name string) []PropertyOption { return slices.Clone(properties[name]) }
func IsProperty(name string) bool                  { _, ok := properties[name]; return ok }
func PropertyValue(name, value string) (string, error) {
	if value == "" {
		value = "none"
	}
	for _, o := range properties[name] {
		if o.Value == value {
			return value, nil
		}
	}
	return "", field(name, "Choose a supported "+name+" value.")
}
func PropertyLabel(name, value string) string {
	for _, o := range properties[name] {
		if o.Value == value {
			return o.Label
		}
	}
	return "None"
}
func ValidatePropertyFilters(priorities, types, sizes []string) error {
	for name, values := range map[string][]string{"priority": priorities, "type": types, "size": sizes} {
		if len(values) > 100 {
			return field(name, "Select at most 100 values.")
		}
		for _, v := range values {
			if v == "" {
				return field(name, "Use none for an unset value.")
			}
			if _, e := PropertyValue(name, v); e != nil {
				return e
			}
		}
	}
	return nil
}
