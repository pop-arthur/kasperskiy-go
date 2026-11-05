package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	// ErrThreatNotFound означает отсутствие записи с запрошенным MD5.
	ErrThreatNotFound = errors.New("threat not found")
	// ErrThreatAlreadyExists означает конфликт при создании записи с существующим MD5.
	ErrThreatAlreadyExists = errors.New("threat already exists")
	// ErrInvalidThreatData означает нарушение ограничений изменяемых полей угрозы.
	ErrInvalidThreatData = errors.New("invalid threat data")
)

// Threat представляет сохранённую угрозу с метками времени, назначенными хранилищем.
type Threat struct {
	// MD5 — канонический идентификатор угрозы.
	MD5 MD5
	// Name — имя угрозы; пустая строка допустима.
	Name string
	// Description содержит описание; nil означает отсутствие, указатель на "" — пустое описание.
	Description *string
	// CreatedAt — время создания, назначаемое хранилищем и сохраняемое при обновлении.
	CreatedAt time.Time
	// UpdatedAt — время последнего обновления; не является версией записи.
	UpdatedAt time.Time
}

// ThreatData содержит изменяемые поля для создания или полной замены данных угрозы.
type ThreatData struct {
	// Name заменяет имя целиком; пробелы и пустая строка сохраняются.
	Name string
	// Description заменяет описание целиком; nil удаляет прежнее значение при Update.
	Description *string
}

// Validate запрещает U+0000 и возвращает ErrInvalidThreatData; пустые строки и пробелы допустимы.
func (data ThreatData) Validate() error {
	if strings.ContainsRune(data.Name, '\x00') {
		return fmt.Errorf("%w: name contains U+0000", ErrInvalidThreatData)
	}
	if data.Description != nil && strings.ContainsRune(*data.Description, '\x00') {
		return fmt.Errorf("%w: description contains U+0000", ErrInvalidThreatData)
	}
	return nil
}
