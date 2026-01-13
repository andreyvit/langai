package langai

import (
	"bytes"
	_ "embed"
	"math"
	"strings"
	"sync"
	"unicode"
)

// EstimateTokens returns an approximate token count for the given text.
// Uses an older GPT-style BPE tokenizer. The count is approximate because:
// - Different models use different tokenizers
// - This tokenizer is from an older OpenAI model
// Generally overestimates slightly, which is fine for threshold checks.
func EstimateTokens(text string) int {
	var result int
	encodeEnum(text, func(token int) {
		result++
	})
	return result
}

//go:embed tokenizer-tokens.bin
var rawTokens []byte

//go:embed tokenizer-bpe.bin
var rawMerges string

var (
	encoderOnce    sync.Once
	bpeMerges      []*bpeMerge
	bpeMergeIndex  map[bpePair]int
	tokenEncodings map[string]int
	byteEncoding   [256]rune
)

func initEncoder() {
	encoderOnce.Do(func() {
		tokenEncodings = make(map[string]int)
		for i, token := range bytes.Split(rawTokens, []byte{0}) {
			if len(token) == 0 {
				continue
			}
			tokenEncodings[string(token)] = i
		}

		for _, line := range strings.FieldsFunc(rawMerges, isNewLine)[1:] {
			first, second, ok := strings.Cut(line, " ")
			if !ok {
				panic("invalid bpe line")
			}
			bpeMerges = append(bpeMerges, &bpeMerge{first, second, first + second})
		}
		bpeMergeIndex = make(map[bpePair]int)
		for i, merge := range bpeMerges {
			bpeMergeIndex[bpePair{merge.First, merge.Second}] = i
		}

		// UTF8 bytes to code points encoding
		for b := '!'; b <= '~'; b++ {
			byteEncoding[b] = rune(b)
		}
		for b := '¡'; b <= '¬'; b++ {
			byteEncoding[b] = rune(b)
		}
		for b := '®'; b <= 'ÿ'; b++ {
			byteEncoding[b] = rune(b)
		}
		var next rune = 256
		for b := 0; b <= 255; b++ {
			if byteEncoding[b] == 0 {
				byteEncoding[b] = next
				next++
			}
		}
	})
}

func encodeEnum(text string, f func(int)) {
	initEncoder()
	split(text, func(chunk string) {
		var tokens []string
		for _, b := range []byte(chunk) {
			tokens = append(tokens, string(byteEncoding[b]))
		}
		tokens = bpe(tokens)
		for _, token := range tokens {
			if v, ok := tokenEncodings[token]; ok {
				f(v)
			}
		}
	})
}

type bpeMerge struct {
	First  string
	Second string
	Result string
}

type bpePair struct {
	First  string
	Second string
}

// bpe merges consecutive pairs of tokens according to bpeMerges
// until no further merging is possible.
func bpe(tokens []string) []string {
	for {
		merge := findBestMerge(tokens)
		if merge == nil {
			break
		}
		tokens = mergeAll(tokens, merge)
	}
	return tokens
}

func findBestMerge(tokens []string) *bpeMerge {
	n := len(tokens)
	var best = math.MaxInt
	for i := 1; i < n; i++ {
		i, ok := bpeMergeIndex[bpePair{tokens[i-1], tokens[i]}]
		if ok && i < best {
			best = i
		}
	}
	if best < math.MaxInt {
		return bpeMerges[best]
	}
	return nil
}

func mergeAll(tokens []string, merge *bpeMerge) []string {
	n := len(tokens)
	dst := 0
	for src := 0; src < n; src++ {
		if tokens[src] == merge.First && src+1 < n && tokens[src+1] == merge.Second {
			tokens[dst] = merge.Result
			src++
		} else {
			tokens[dst] = tokens[src]
		}
		dst++
	}
	return tokens[:dst]
}

// split enumerates consecutive token candidates (chunks) in a string.
// Handles contractions, letter runs, number runs, and whitespace.
func split(text string, f func(chunk string)) {
	var state splitState
	var start int

	flush := func(end int) {
		if end > start {
			f(text[start:end])
			start = end
		}
	}

	for pos, r := range text {
		isS, isL, isN := unicode.IsSpace(r), unicode.IsLetter(r), unicode.IsNumber(r)
		again := true
		for again {
			again = false
			switch state {
			case initial:
				if r == '\'' {
					state = afterApostrophe
				} else if r == ' ' {
					state = afterSpace
				} else if isS {
					state = inWhitespaceTokenAfterOtherWhitespace
				} else if isL {
					state = inLetterToken
				} else if isN {
					state = inNumberToken
				} else {
					state = inOtherToken
				}
			case afterApostrophe:
				if r == 's' || r == 't' || r == 'm' || r == 'd' {
					flush(pos + 1)
					state = initial
				} else if r == 'r' || r == 'v' {
					state = afterApostropheNeedE
				} else if r == 'l' {
					state = afterApostropheNeedL
				} else {
					state, again = inOtherToken, true
				}
			case afterApostropheNeedE:
				if r == 'e' {
					flush(pos + 1)
					state = initial
				} else {
					state, again = inOtherToken, true
				}
			case afterApostropheNeedL:
				if r == 'l' {
					flush(pos + 1)
					state = initial
				} else {
					state, again = inOtherToken, true
				}
			case afterSpace:
				if r == ' ' {
					state = inWhitespaceTokenAfterSpace
				} else if isS {
					state = inWhitespaceTokenAfterOtherWhitespace
				} else if isL {
					state = inLetterToken
				} else if isN {
					state = inNumberToken
				} else {
					state = inOtherToken
				}
			case inWhitespaceTokenAfterOtherWhitespace:
				if r == ' ' {
					state = inWhitespaceTokenAfterSpace
				} else if !isS {
					flush(pos)
					state, again = initial, true
				}
			case inWhitespaceTokenAfterSpace:
				if r == ' ' {
					// nop
				} else if isS {
					state = inWhitespaceTokenAfterOtherWhitespace
				} else {
					flush(pos - 1)
					state, again = initial, true
				}
			case inLetterToken:
				if !isL {
					flush(pos)
					state, again = initial, true
				}
			case inNumberToken:
				if !isN {
					flush(pos)
					state, again = initial, true
				}
			case inOtherToken:
				if isS || isL || isN {
					flush(pos)
					state, again = initial, true
				}
			}
		}
	}
	flush(len(text))
}

type splitState int

const (
	initial = splitState(iota)
	afterApostrophe
	afterApostropheNeedE
	afterApostropheNeedL
	afterSpace
	inWhitespaceTokenAfterSpace
	inWhitespaceTokenAfterOtherWhitespace
	inLetterToken
	inNumberToken
	inOtherToken
)

func isNewLine(r rune) bool {
	return r == '\n'
}
