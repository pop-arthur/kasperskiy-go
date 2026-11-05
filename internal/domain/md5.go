// Package domain содержит модель угрозы, её проверки и ошибки.
package domain

import (
	"errors"
	"strings"
)

// ErrInvalidMD5 означает, что значение не содержит ровно 32 ASCII hex-символа.
var ErrInvalidMD5 = errors.New("invalid MD5")

// MD5 хранит хеш в нижнем регистре; проверенное значение создаёт ParseMD5.
type MD5 string

// ParseMD5 принимает ровно 32 hex-символа без пробелов, приводит их к нижнему регистру или возвращает ErrInvalidMD5.
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
