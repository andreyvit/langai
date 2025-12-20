package langai

import "testing"

func TestToolCallUnmarshalInput(t *testing.T) {
	call := ToolCall{Name: "t", Input: []byte(`{"a":1}`)}
	var out struct {
		A int `json:"a"`
	}
	if err := call.UnmarshalInput(&out); err != nil {
		t.Fatal(err)
	}
	if out.A != 1 {
		t.Fatalf("unexpected output: %+v", out)
	}
}
