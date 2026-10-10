package main

import (
	"regexp"
)

type graphicsOSInspection struct {
	Failure          string             `json:"failure_code,omitempty"`
	FileSHA          string             `json:"file_sha256,omitempty"`
	FinalPathMatches bool               `json:"final_path_matches_observed"`
	Signature        *graphicsSignature `json:"signature,omitempty"`
	NaturalCleanup   bool               `json:"verifier_natural_cleanup"`
	Accepted         bool               `json:"module_accepted"`
}

type graphicsSignature struct {
	Schema     int    `json:"schema_version"`
	Status     int    `json:"status_code"`
	OSBinary   bool   `json:"is_os_binary"`
	Type       string `json:"signature_type"`
	Publisher  string `json:"publisher_name"`
	Issuer     string `json:"issuer_name"`
	Thumbprint string `json:"signer_thumbprint"`
	FileSHA    string `json:"file_sha256"`
}

var signatureLabel = regexp.MustCompile(`^[A-Za-z0-9 .(),+_-]{0,128}$`)
var certificateThumbprint = regexp.MustCompile(`^[0-9a-f]{40}$`)

func parseGraphicsSignature(raw []byte, digest string) (*graphicsSignature, error) {
	var r graphicsSignature
	if len(raw) > 2048 || exactJSON(raw, &r, "schema_version", "status_code", "is_os_binary", "signature_type", "publisher_name", "issuer_name", "signer_thumbprint", "file_sha256") != nil ||
		r.Schema != 1 || r.Status < 0 || r.Status > 6 || !hashPattern.MatchString(digest) || r.FileSHA != digest ||
		!signatureLabel.MatchString(r.Publisher) || !signatureLabel.MatchString(r.Issuer) ||
		(r.Type != "Authenticode" && r.Type != "Catalog" && r.Type != "None") ||
		(r.Thumbprint != "" && !certificateThumbprint.MatchString(r.Thumbprint)) {
		return nil, failure("invalid_os_signature_inspection")
	}
	if r.Status == 0 && (r.Publisher == "" || r.Issuer == "" || r.Thumbprint == "" || r.Type == "None") {
		return nil, failure("invalid_os_signature_inspection")
	}
	return &r, nil
}

// The fixed script uses the existing Windows trust policy, including catalog
// signatures. The inspected path arrives only on private stdin. It neither
// installs certificates nor suppresses trust/revocation/signature failures.
const graphicsSignatureScript = `$ErrorActionPreference='Stop'; $ProgressPreference='SilentlyContinue';
try {
 $request = [Console]::In.ReadLine() | ConvertFrom-Json
 $signature = Get-AuthenticodeSignature -LiteralPath $request.path
 $publisher=''; $issuer=''; $thumbprint=''
 if ($null -ne $signature.SignerCertificate) {
  $certificate=$signature.SignerCertificate
  $publisher=$certificate.GetNameInfo([System.Security.Cryptography.X509Certificates.X509NameType]::SimpleName,$false)
  $issuer=$certificate.GetNameInfo([System.Security.Cryptography.X509Certificates.X509NameType]::SimpleName,$true)
  $thumbprint=$certificate.Thumbprint.ToLowerInvariant()
 }
 $result=[ordered]@{schema_version=1;status_code=[int]$signature.Status;is_os_binary=[bool]$signature.IsOSBinary;signature_type=[string]$signature.SignatureType;publisher_name=$publisher;issuer_name=$issuer;signer_thumbprint=$thumbprint;file_sha256=(Get-FileHash -Algorithm SHA256 -LiteralPath $request.path).Hash.ToLowerInvariant()}
 [Console]::Out.WriteLine(($result | ConvertTo-Json -Compress)); exit 0
} catch { exit 2 }`

func graphicsSignatureArguments() []string {
	return []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", graphicsSignatureScript}
}
