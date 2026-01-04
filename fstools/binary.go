package fstools

import (
	"bytes"
	"unicode/utf8"
)

const binarySampleBytes = 8 * 1024

func isBinaryFileBytes(b []byte) bool {
	sample := b
	truncated := false
	if len(sample) > binarySampleBytes {
		sample = sample[:binarySampleBytes]
		truncated = true
	}

	if bytes.IndexByte(sample, 0) >= 0 {
		return true
	}

	if utf8.Valid(sample) {
		return false
	}

	if !truncated {
		return true
	}

	for cut := 1; cut < utf8.UTFMax && cut < len(sample); cut++ {
		prefix := sample[:len(sample)-cut]
		suffix := sample[len(sample)-cut:]
		if !utf8.Valid(prefix) {
			continue
		}
		if validTruncatedUTF8Suffix(suffix) {
			return false
		}
	}

	return true
}

func validTruncatedUTF8Suffix(suffix []byte) bool {
	if len(suffix) == 0 {
		return false
	}

	need := utf8ExpectedLen(suffix[0])
	if need == 0 {
		return false
	}
	if len(suffix) >= need {
		return false
	}

	if len(suffix) >= 2 {
		if !isCont(suffix[1]) {
			return false
		}
		switch suffix[0] {
		case 0xE0:
			if suffix[1] < 0xA0 {
				return false
			}
		case 0xED:
			if suffix[1] > 0x9F {
				return false
			}
		case 0xF0:
			if suffix[1] < 0x90 {
				return false
			}
		case 0xF4:
			if suffix[1] > 0x8F {
				return false
			}
		}
	}

	if len(suffix) >= 3 && !isCont(suffix[2]) {
		return false
	}
	if len(suffix) >= 4 && !isCont(suffix[3]) {
		return false
	}

	return true
}

func utf8ExpectedLen(b0 byte) int {
	switch {
	case b0 < 0x80:
		return 1
	case 0xC2 <= b0 && b0 <= 0xDF:
		return 2
	case 0xE0 <= b0 && b0 <= 0xEF:
		return 3
	case 0xF0 <= b0 && b0 <= 0xF4:
		return 4
	default:
		return 0
	}
}

func isCont(b byte) bool {
	return 0x80 <= b && b <= 0xBF
}
