//go:build vm330_signature_probe

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestVM330StartupDiagnosticClosedCategories(t *testing.T) {
	policy := []byte("C:\\private\\user\\file.ps1 cannot be loaded because running scripts is disabled on this system. PSSecurityException FullyQualifiedErrorId : UnauthorizedAccess")
	for _, tc := range []struct {
		name     string
		raw      []byte
		err      error
		category string
	}{
		{"policy", policy, nil, "disabled_script_policy_pattern"},
		{"partial", []byte("PSSecurityException UnauthorizedAccess"), nil, "unclassified"},
		{"unrecognized", []byte("C:/private/secret other failure"), nil, "unclassified"},
		{"wrong_token", []byte("PSSecurityExceptionSuffix UnauthorizedAccess running scripts is disabled on this system"), nil, "unclassified"},
		{"parser_and_policy", append(append([]byte(nil), policy...), []byte(" ParserError")...), nil, "multiple_categories"},
		{"command_and_policy", append(append([]byte(nil), policy...), []byte(" CommandNotFoundException")...), nil, "multiple_categories"},
		{"invalid_encoding", append(append([]byte(nil), policy...), 0xff), nil, "invalid_encoding"},
		{"truncation", append(bytes.Repeat([]byte{'x'}, 65537), policy...), nil, "truncated"},
		{"read_failed", policy, errors.New("private path"), "read_failed"},
		{"empty", nil, nil, "empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := classifySignatureStartup(tc.raw, tc.err)
			if r.Category != tc.category {
				t.Fatal("diagnostic_category_mismatch")
			}
			b, e := json.Marshal(r)
			if e != nil || strings.Contains(string(b), "private") || strings.Contains(string(b), "file.ps1") || strings.Contains(string(b), "secret") {
				t.Fatal("diagnostic_disclosed_text")
			}
			if tc.category == "truncated" || tc.category == "invalid_encoding" || tc.category == "read_failed" {
				if r.SecurityException || r.UnauthorizedAccess || r.DisabledScripts || r.ParserError || r.CommandNotFound {
					t.Fatal("incomplete_diagnostic_classified")
				}
			}
		})
	}
}
