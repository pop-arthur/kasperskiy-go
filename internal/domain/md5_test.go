package domain_test

import (
	"errors"
	"strings"
	"testing"

	"kaspersky-go-test/internal/domain"
)

func TestParseMD5(t *testing.T) {
	const canonical = "d41d8cd98f00b204e9800998ecf8427e"
	for _, value := range []string{canonical, strings.ToUpper(canonical), "D41d8cD98f00b204e9800998ecf8427E", strings.Repeat("0", 32)} {
		t.Run(value, func(t *testing.T) {
			got, err := domain.ParseMD5(value)
			if err != nil || string(got) != strings.ToLower(value) {
				t.Fatalf("ParseMD5 = %q, %v", got, err)
			}
		})
	}
	for _, value := range []string{"", "short", strings.Repeat("0", 31), strings.Repeat("0", 33), strings.Repeat("g", 32), canonical + "\n", " " + canonical, strings.Repeat("я", 16)} {
		t.Run("invalid/"+value, func(t *testing.T) {
			got, err := domain.ParseMD5(value)
			if !errors.Is(err, domain.ErrInvalidMD5) || got != "" {
				t.Fatalf("ParseMD5 = %q, %v; want invalid MD5", got, err)
			}
		})
	}
}
