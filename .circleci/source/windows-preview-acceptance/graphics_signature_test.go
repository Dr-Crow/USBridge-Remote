package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGraphicsSignatureInspectionParsingAndTrustPolicy(t *testing.T) {
	digest := strings.Repeat("a", 64)
	r := graphicsSignature{Schema: 1, Status: 0, Type: "Catalog", Publisher: "Microsoft Windows", Issuer: "Microsoft Windows Production PCA 2011", Thumbprint: strings.Repeat("b", 40), FileSHA: digest}
	good, _ := json.Marshal(r)
	if _, err := parseGraphicsSignature(good, digest); err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"read_request", "query_signature", "read_certificate", "hash_file", "write_result"} {
		_, err := parseGraphicsSignature([]byte(`{"schema_version":1,"failure_stage":"`+stage+`"}`), digest)
		if err == nil || err.Error() != "os_verifier_"+stage+"_failed" {
			t.Fatal("closed failure stage lost")
		}
	}
	for _, raw := range [][]byte{
		append(append([]byte{}, good...), []byte(`{}`)...),
		[]byte(strings.Replace(string(good), `"schema_version":1`, `"schema_version":1,"schema_version":1`, 1)),
		[]byte(strings.Replace(string(good), `"schema_version":1`, `"SCHEMA_VERSION":1`, 1)),
		[]byte(strings.Replace(string(good), `"status_code":0,`, ``, 1)),
		[]byte(strings.Replace(string(good), `"is_os_binary":false`, `"is_os_binary":null`, 1)),
		[]byte(strings.Replace(string(good), digest, strings.Repeat("c", 64), 1)),
		[]byte(strings.Replace(string(good), `Microsoft Windows"`, `C:\\private\\file.dll"`, 1)),
	} {
		if _, err := parseGraphicsSignature(raw, digest); err == nil {
			t.Fatal("invalid signature inspection accepted")
		}
	}
	for _, mutate := range []func(*graphicsSignature){func(v *graphicsSignature) { v.Status = 7 }, func(v *graphicsSignature) { v.Thumbprint = "" }, func(v *graphicsSignature) { v.Type = "None" }, func(v *graphicsSignature) { v.Publisher = "" }} {
		v := r
		mutate(&v)
		b, _ := json.Marshal(v)
		if _, err := parseGraphicsSignature(b, digest); err == nil {
			t.Fatal("invalid successful inspection accepted")
		}
	}
	// A failed trust verdict is recorded as a failed verdict, never rewritten.
	r.Status = 4
	b, _ := json.Marshal(r)
	v, err := parseGraphicsSignature(b, digest)
	if err != nil || v.Status != 4 {
		t.Fatal("trust failure hidden")
	}
	args := strings.Join(graphicsSignatureArguments(`C:\owned\verify-os-file.ps1`), " ") + " " + graphicsSignatureScript
	if !strings.Contains(args, "-File C:\\owned\\verify-os-file.ps1") || strings.Contains(args, "-Command") {
		t.Fatal("fixed local script invocation required")
	}
	for _, required := range []string{"-NoProfile", "-NonInteractive", "[Console]::In.ReadLine()", "[Console]::Out.WriteLine", "Get-AuthenticodeSignature -LiteralPath $request.path", "Get-FileHash -Algorithm SHA256 -LiteralPath $request.path"} {
		if !strings.Contains(args, required) {
			t.Fatal("missing bounded verifier contract")
		}
	}
	for _, forbidden := range []string{"ExecutionPolicy", "Bypass", "SkipCertificate", "Import-Certificate", "Set-Item", "Add-Type", "Invoke-Expression"} {
		if strings.Contains(args, forbidden) {
			t.Fatal("trust or execution policy changed")
		}
	}
}

func TestSignatureScriptWaitsForPrivateQueryAfterLiteral(t *testing.T) {
	marker := strings.Index(graphicsSignatureScript, "[Console]::Out.WriteLine('"+signatureStartupMarker+"')")
	input := strings.Index(graphicsSignatureScript, "[Console]::In.ReadLine()")
	query := strings.Index(graphicsSignatureScript, "Get-AuthenticodeSignature")
	if marker < 0 || input <= marker || query <= input {
		t.Fatal("signature query precedes owned startup handshake")
	}
}
