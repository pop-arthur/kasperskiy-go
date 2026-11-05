package httptransport

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode/utf8"
)

func decodeObject(w http.ResponseWriter, r *http.Request, maxBytes int64) (map[string]json.RawMessage, error) {
	if wrapped, ok := w.(interface{ Unwrap() http.ResponseWriter }); ok {
		w = wrapped.Unwrap()
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	defer r.Body.Close()
	mediaType, parameters, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return nil, errors.New("Content-Type must be application/json")
	}
	if charset, ok := parameters["charset"]; ok && !strings.EqualFold(charset, "utf-8") {
		return nil, errors.New("charset must be UTF-8")
	}
	if encoding := r.Header.Get("Content-Encoding"); encoding != "" && !strings.EqualFold(encoding, "identity") {
		return nil, errors.New("Content-Encoding is not supported")
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var oversized *http.MaxBytesError
		if errors.As(err, &oversized) {
			return nil, errors.New("request body exceeds the size limit")
		}
		return nil, errors.New("could not read request body")
	}
	if !utf8.Valid(body) {
		return nil, errors.New("JSON must be UTF-8")
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(body, &object); err != nil || object == nil {
		return nil, errors.New("body must contain exactly one JSON object")
	}
	return object, nil
}

// readString возвращает nil для отсутствующего необязательного поля, сохраняет пустую строку и отклоняет null.
func readString(object map[string]json.RawMessage, field string, required bool) (*string, error) {
	raw, exists := object[field]
	if !exists {
		if required {
			return nil, fmt.Errorf("%s is required", field)
		}
		return nil, nil
	}
	var value string

	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &value) != nil {
		return nil, fmt.Errorf("%s must be a string", field)
	}
	return &value, nil
}

func decodeCreate(object map[string]json.RawMessage) (ThreatCreateRequest, error) {
	md5, err := readString(object, "md5", true)
	if err != nil {
		return ThreatCreateRequest{}, err
	}
	fields, err := decodeUpdate(object)
	if err != nil {
		return ThreatCreateRequest{}, err
	}
	return ThreatCreateRequest{MD5: *md5, Name: fields.Name, Description: fields.Description}, nil
}

func decodeUpdate(object map[string]json.RawMessage) (ThreatUpdateRequest, error) {
	name, err := readString(object, "name", true)
	if err != nil {
		return ThreatUpdateRequest{}, err
	}
	description, err := readString(object, "description", false)
	if err != nil {
		return ThreatUpdateRequest{}, err
	}
	return ThreatUpdateRequest{Name: *name, Description: description}, nil
}
