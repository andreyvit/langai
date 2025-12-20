package langai

import "testing"

func TestMarshalJSON_NoEscapeHTML(t *testing.T) {
	b, err := MarshalJSON(map[string]string{"x": "<tag>"})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"x":"<tag>"}` {
		t.Fatalf("unexpected JSON: %s", b)
	}
}
