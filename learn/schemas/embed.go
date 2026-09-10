// Package schemas exposes the documented wire contract to runtime validation.
package schemas

import _ "embed"

//go:embed thread-frame.schema.json
var ThreadFrame []byte
