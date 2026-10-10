// Package componentjson provides unambiguous, bounded-protocol JSON decoding.
package componentjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
)

// Decode rejects duplicate object fields, unknown or non-exact typed field
// names and trailing values. Callers must limit the raw byte length. Protocol
// structs use named fields; anonymous field promotion is intentionally unsupported.
func Decode(raw []byte, out any) error {
	target := reflect.TypeOf(out)
	if target == nil || target.Kind() != reflect.Pointer || reflect.ValueOf(out).IsNil() {
		return errors.New("expected nonnil typed JSON destination")
	}
	scan := json.NewDecoder(bytes.NewReader(raw))
	token, err := scan.Token()
	if err != nil || token != json.Delim('{') {
		return errors.New("expected JSON object")
	}
	if err := object(scan, target.Elem()); err != nil {
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
func indirect(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func object(d *json.Decoder, target reflect.Type) error {
	target = indirect(target)
	var fields map[string]reflect.Type
	if target != nil && target.Kind() == reflect.Struct {
		fields = map[string]reflect.Type{}
		for i := 0; i < target.NumField(); i++ {
			field := target.Field(i)
			if field.PkgPath != "" {
				continue
			}
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name == "-" {
				continue
			}
			if field.Anonymous {
				return errors.New("anonymous JSON protocol field unsupported")
			}
			if name == "" {
				name = field.Name
			}
			if _, exists := fields[name]; exists {
				return errors.New("ambiguous JSON protocol schema")
			}
			fields[name] = field.Type
		}
	}
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
		var child reflect.Type
		if fields != nil {
			var exists bool
			child, exists = fields[key]
			if !exists {
				return errors.New("unknown or noncanonical JSON field")
			}
		} else if target != nil && target.Kind() == reflect.Map {
			child = target.Elem()
		}
		if err := value(d, child); err != nil {
			return err
		}
	}
	token, err := d.Token()
	if err != nil || token != json.Delim('}') {
		return errors.New("invalid JSON object")
	}
	return nil
}
func value(d *json.Decoder, target reflect.Type) error {
	token, err := d.Token()
	if err != nil {
		return err
	}
	switch token {
	case json.Delim('{'):
		return object(d, target)
	case json.Delim('['):
		target = indirect(target)
		var element reflect.Type
		if target != nil && (target.Kind() == reflect.Slice || target.Kind() == reflect.Array) {
			element = target.Elem()
		}
		for d.More() {
			if err := value(d, element); err != nil {
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
