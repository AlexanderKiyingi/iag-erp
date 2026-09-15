package store

import "encoding/json"

// marshalAttrs is scanAttrs' other half: nil and unmarshalable both become an
// empty object, because the column is NOT NULL and a write that fails to encode
// must not take the row down with it.
func marshalAttrs(m map[string]any) []byte {
	if m == nil {
		return []byte("{}")
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return []byte("{}")
	}
	return raw
}

func scanAttrs(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return map[string]any{}
	}
	return m
}
