package main

// This closed observation is absent in normal builds. It never accepts stderr
// or replaces a primary startup failure; no raw text or paths are retained.
type signatureStartupDiagnostic struct {
	Bytes              int64  `json:"stderr_bytes"`
	ReadFailed         bool   `json:"read_failed"`
	Truncated          bool   `json:"truncated"`
	ValidUTF8          bool   `json:"valid_utf8"`
	SecurityException  bool   `json:"security_exception_pattern"`
	UnauthorizedAccess bool   `json:"unauthorized_access_pattern"`
	DisabledScripts    bool   `json:"disabled_scripts_pattern"`
	ParserError        bool   `json:"parser_error_pattern"`
	CommandNotFound    bool   `json:"command_not_found_pattern"`
	Category           string `json:"category"`
}
