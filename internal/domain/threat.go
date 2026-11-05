package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrThreatNotFound      = errors.New("threat not found")
	ErrThreatAlreadyExists = errors.New("threat already exists")
	ErrInvalidThreatData   = errors.New("invalid threat data")
)

// Threat is a persisted threat. Timestamps are assigned by the database.
type Threat struct {
	MD5         MD5
	Name        string
	Description *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ThreatData contains the fields replaced by an update.
// A nil Description means no description; a pointer to "" means empty text.
type ThreatData struct {
	Name        string
	Description *string
}

// Validate checks storage-compatible text without trimming or rejecting empty names.
// JSON field presence and explicit null are checked by the HTTP transport.
func (data ThreatData) Validate() error {
	if strings.ContainsRune(data.Name, '\x00') {
		return fmt.Errorf("%w: name contains U+0000", ErrInvalidThreatData)
	}
	if data.Description != nil && strings.ContainsRune(*data.Description, '\x00') {
		return fmt.Errorf("%w: description contains U+0000", ErrInvalidThreatData)
	}
	return nil
}
