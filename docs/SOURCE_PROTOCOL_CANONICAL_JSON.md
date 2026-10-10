# Canonical local component protocol input

The experimental agent supervisors accept component protocol JSON over private
stdin/stdout. The Go JSON decoder accepts case-insensitive struct field matches,
including some Unicode folds, even with `DisallowUnknownFields`. Duplicate-name
checking alone did not reject `capture_consent:false` followed by
`CAPTURE_CONSENT:true` in the agent launch decoder. A regression test reproduced
that interpretation against the prior public agent code.

The agent's typed scanner now accepts only exact declared field names at every
typed object level. It retains duplicate, unknown and trailing-value rejection;
arrays, pointers and typed map values preserve their schema. Dynamic string-map
keys remain data. Protocol structs use named fields; anonymous-field promotion is
unsupported. Normal lower-snake-case output from the agent is unchanged.

Go's strict standard base64 decoder checks padding bits but still ignores CR/LF.
The streamer launch key, viewer descriptor key, broker transfer payload and broker
completion payload now also require the exact encoded length of the decoded
bytes. This rejects alternate newline encodings without creating another
immutable copy of the key or payload. Existing byte-size limits still apply.

The client preview descriptor already checked exact field names. New regression
coverage confirms that behavior and fixes its separate CR/LF key acceptance.
Errors remain generic; session keys and descriptors are not recorded.

This is trusted local protocol interpretation hardening. It does not establish
a remote authentication bypass or add a remote endpoint, pairing authority,
capture permission, device consent or persistent credential.

Validation includes before/after alias rejection, nested typed objects, canonical
base64 round trips, existing supervisor/helper-process lifecycles, full agent race
tests/vet, client race tests/vet and a bounded client decoder fuzz smoke. Hosted
native acceptance must pass for the exact published commit separately.
