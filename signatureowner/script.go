// SPDX-License-Identifier: GPL-3.0-only
package signatureowner

// SignatureScriptSHA256 identifies the original source-owned query example.
const SignatureScriptSHA256 = "f8af1cb208563f542d50b40d7c2b2959f5baa3ecc64518f5df3ad5b67a0805fb"

// SignatureScript must be staged verbatim at Spec.ScriptPath by the caller.
// It uses existing Windows trust policy. A valid signature is evidence only;
// the caller must independently decide publisher trust and authorization.
const SignatureScript = `$ErrorActionPreference='Stop'; $ProgressPreference='SilentlyContinue'; $stage='read_request';
try {
 [Console]::Out.WriteLine('signature_owner_ready_v1')
 $request = [Console]::In.ReadLine() | ConvertFrom-Json
 $stage='query_signature'
 $signature = Get-AuthenticodeSignature -LiteralPath $request.path
 $stage='read_certificate'
 $publisher=''; $issuer=''; $thumbprint=''
 if ($null -ne $signature.SignerCertificate) {
  $certificate=$signature.SignerCertificate
  $publisher=$certificate.GetNameInfo([System.Security.Cryptography.X509Certificates.X509NameType]::SimpleName,$false)
  $issuer=$certificate.GetNameInfo([System.Security.Cryptography.X509Certificates.X509NameType]::SimpleName,$true)
  $thumbprint=$certificate.Thumbprint.ToLowerInvariant()
 }
 $stage='hash_file'
 $digest=(Get-FileHash -Algorithm SHA256 -LiteralPath $request.path).Hash.ToLowerInvariant()
 $stage='write_result'
 $result=[ordered]@{schema_version=1;status_code=[int]$signature.Status;is_os_binary=[bool]$signature.IsOSBinary;signature_type=[string]$signature.SignatureType;publisher_name=$publisher;issuer_name=$issuer;signer_thumbprint=$thumbprint;file_sha256=$digest}
 [Console]::Out.WriteLine(($result | ConvertTo-Json -Compress))
 exit 0
} catch { [Console]::Out.WriteLine(([ordered]@{schema_version=1;failure_stage=$stage} | ConvertTo-Json -Compress)); exit 0 }`

func signatureArguments(script string) []string {
	return []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-File", script}
}
