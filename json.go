package langai

import (
	"bytes"
	"encoding/json"
)

func MarshalJSON(v any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSpace(buf.Bytes()), nil
}

func MustMarshalJSON(v any) json.RawMessage {
	b, err := MarshalJSON(v)
	if err != nil {
		panic(err)
	}
	return b
}
