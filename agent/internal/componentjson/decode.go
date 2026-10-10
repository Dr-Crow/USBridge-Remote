// Package componentjson provides unambiguous, bounded-protocol JSON decoding.
package componentjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// Decode rejects duplicate object fields, unknown typed fields and trailing
// values. Callers are responsible for limiting the raw byte length.
func Decode(raw []byte, out any) error {
	scan := json.NewDecoder(bytes.NewReader(raw))
	token, err := scan.Token()
	if err != nil || token != json.Delim('{') {
		return errors.New("expected JSON object")
	}
	if err := object(scan); err != nil {
		return err
	}
	if _, err := scan.Token(); err != io.EOF {
		return errors.New("trailing JSON data")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("trailing JSON data")
	}
	return nil
}
func object(d *json.Decoder) error {
	seen := map[string]bool{}
	for d.More() {
		token, err := d.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return errors.New("duplicate JSON field")
		}
		seen[key] = true
		if err := value(d); err != nil {
			return err
		}
	}
	token, err := d.Token()
	if err != nil || token != json.Delim('}') {
		return errors.New("invalid JSON object")
	}
	return nil
}
func value(d *json.Decoder) error {
	token, err := d.Token()
	if err != nil {
		return err
	}
	switch token {
	case json.Delim('{'):
		return object(d)
	case json.Delim('['):
		for d.More() {
			if err := value(d); err != nil {
				return err
			}
		}
		token, err = d.Token()
		if err != nil || token != json.Delim(']') {
			return errors.New("invalid JSON array")
		}
	}
	return nil
}
