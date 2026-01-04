package fstools

import (
	"bytes"
	"testing"
)

func TestIsBinaryFileBytes_Text(t *testing.T) {
	if isBinaryFileBytes([]byte("hello\nworld\n")) {
		t.Fatal("expected text to be non-binary")
	}
}

func TestIsBinaryFileBytes_NULIsBinary(t *testing.T) {
	if !isBinaryFileBytes([]byte("a\x00b")) {
		t.Fatal("expected NUL-containing content to be binary")
	}
}

func TestIsBinaryFileBytes_InvalidUTF8SmallIsBinary(t *testing.T) {
	if !isBinaryFileBytes([]byte{0xff, 'x'}) {
		t.Fatal("expected invalid UTF-8 content to be binary")
	}
}

func TestIsBinaryFileBytes_InvalidUTF8AtBoundaryNotBinary(t *testing.T) {
	b := bytes.Repeat([]byte("a"), binarySampleBytes-1)
	b = append(b, 0xE2)       // start of a 3-byte rune (Euro sign) but cut at boundary
	b = append(b, 0x82, 0xAC) // continuation in the "rest of file"
	b = append(b, '\n')
	if isBinaryFileBytes(b) {
		t.Fatal("expected truncated UTF-8 rune at 8KB boundary to be non-binary")
	}
}

func TestIsBinaryFileBytes_InvalidLeadAtBoundaryIsBinary(t *testing.T) {
	b := bytes.Repeat([]byte("a"), binarySampleBytes-1)
	b = append(b, 0xC0) // invalid UTF-8 lead byte
	b = append(b, 'x', 'y', 'z')
	if !isBinaryFileBytes(b) {
		t.Fatal("expected invalid UTF-8 lead byte at boundary to be binary")
	}
}

func TestIsBinaryFileBytes_InvalidUTF8InPrefixIsBinary(t *testing.T) {
	b := bytes.Repeat([]byte("a"), binarySampleBytes+1)
	b[100] = 0xff
	if !isBinaryFileBytes(b) {
		t.Fatal("expected invalid UTF-8 inside prefix to be binary")
	}
}
