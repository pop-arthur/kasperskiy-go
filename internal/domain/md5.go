package domain

import (
	"errors"
	"strings"
)

var ErrInvalidMD5 = errors.New("invalid MD5")

// MD5 identifies a threat using a canonical lowercase hexadecimal hash.
// Use ParseMD5 to validate external input before calling a repository.
type MD5 string

// ParseMD5 accepts exactly 32 ASCII hexadecimal characters without trimming.
func ParseMD5(value string) (MD5, error) {
	if len(value) != 32 {
		return "", ErrInvalidMD5
	}
	for i := range len(value) {
		c := value[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return "", ErrInvalidMD5
		}
	}
	return MD5(strings.ToLower(value)), nil
}
