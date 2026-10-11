//go:build vm331_signature_probe

package main

import (
	"bytes"
	"regexp"
	"unicode/utf8"
)

var signatureSecurityPattern = regexp.MustCompile(`\bPSSecurityException\b`)
var signatureUnauthorizedPattern = regexp.MustCompile(`\bUnauthorizedAccess\b`)
var signatureParserPattern = regexp.MustCompile(`\bParserError\b`)
var signatureCommandPattern = regexp.MustCompile(`\bCommandNotFoundException\b`)

func classifySignatureStartup(raw []byte, readErr error) signatureStartupDiagnostic {
	r := signatureStartupDiagnostic{Bytes: int64(len(raw)), ReadFailed: readErr != nil, Truncated: len(raw) > 65536, ValidUTF8: utf8.Valid(raw)}
	// Fail conservatively before pattern matching incomplete or undecodable data.
	switch {
	case r.ReadFailed:
		r.Category = "read_failed"
		return r
	case r.Truncated:
		r.Category = "truncated"
		return r
	case !r.ValidUTF8:
		r.Category = "invalid_encoding"
		return r
	case len(raw) == 0:
		r.Category = "empty"
		return r
	}
	r.SecurityException = signatureSecurityPattern.Match(raw)
	r.UnauthorizedAccess = signatureUnauthorizedPattern.Match(raw)
	lower := bytes.ToLower(raw)
	r.DisabledScripts = bytes.Contains(lower, []byte("running scripts is disabled on this system"))
	clear(lower)
	r.ParserError = signatureParserPattern.Match(raw)
	r.CommandNotFound = signatureCommandPattern.Match(raw)
	r.Category = "unclassified"
	if r.SecurityException && r.UnauthorizedAccess && r.DisabledScripts {
		if r.ParserError || r.CommandNotFound {
			r.Category = "multiple_categories"
		} else {
			r.Category = "disabled_script_policy_pattern"
		}
	} else if r.ParserError && r.CommandNotFound {
		r.Category = "multiple_categories"
	}
	return r
}
