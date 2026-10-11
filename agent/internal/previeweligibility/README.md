# Read-only Windows preview eligibility

The same-user Windows manager requires all successful observations: a
non-elevated current process token, a nonzero token session, a visible process
window station, and successful release of the owned token handle. These facts
are prerequisites, not capture consent or a physical-display claim. The borrowed
window-station handle is never closed. No linked/impersonation token is used.

The Windows Manager uses these facts for every Start; its settings action stays
gated until actual staged Manager and full GUI acceptance pass. The hosted CI
account is elevated and is correctly reported as ineligible. An ordinary VM
desktop probe passed all facts without changing identity or security configuration.

Microsoft references:
- https://learn.microsoft.com/en-us/windows/win32/api/securitybaseapi/nf-securitybaseapi-gettokeninformation
- https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-getprocesswindowstation
- https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-getuserobjectinformationw
