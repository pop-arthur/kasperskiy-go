package domain_test

import (
	"errors"
	"testing"

	"kaspersky-go-test/internal/domain"
)

func TestThreatDataValidate(t *testing.T) {
	empty, text, nul := "", " описание\nугрозы ", "bad\x00text"
	for _, tt := range []struct {
		name string
		data domain.ThreatData
		bad  bool
	}{
		{"empty name", domain.ThreatData{}, false},
		{"whitespace", domain.ThreatData{Name: " \n "}, false},
		{"empty description", domain.ThreatData{Name: "Trojan", Description: &empty}, false},
		{"unicode", domain.ThreatData{Name: "Угроза", Description: &text}, false},
		{"nul name", domain.ThreatData{Name: nul}, true},
		{"nul description", domain.ThreatData{Description: &nul}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.data.Validate()
			if tt.bad {
				if !errors.Is(err, domain.ErrInvalidThreatData) {
					t.Fatalf("Validate = %v; want invalid threat data", err)
				}
			} else if err != nil {
				t.Fatalf("Validate = %v", err)
			}
		})
	}
}
