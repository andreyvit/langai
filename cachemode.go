package langai

import (
	"fmt"
	"slices"
)

type CacheMode int

const (
	CacheModeNone CacheMode = iota
	CacheModeShortTerm
	CacheModeLongTerm
)

var _cacheModeStrings = [...]string{
	CacheModeNone:      "none",
	CacheModeShortTerm: "short",
	CacheModeLongTerm:  "long",
}

func (v CacheMode) String() string {
	if v < 0 || int(v) >= len(_cacheModeStrings) {
		return "none"
	}
	return _cacheModeStrings[v]
}

func ParseCacheMode(s string) (CacheMode, error) {
	if i := slices.Index(_cacheModeStrings[:], s); i >= 0 {
		return CacheMode(i), nil
	}
	return CacheModeNone, fmt.Errorf("invalid CacheMode %q", s)
}

func (v CacheMode) MarshalText() ([]byte, error) {
	return []byte(v.String()), nil
}

func (v *CacheMode) UnmarshalText(b []byte) error {
	var err error
	*v, err = ParseCacheMode(string(b))
	return err
}
