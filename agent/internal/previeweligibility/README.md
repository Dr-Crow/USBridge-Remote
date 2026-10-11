# Read-only Windows preview eligibility

A future same-user manager must require all successful observations: a
non-elevated current process token, a nonzero token session, a visible process
window station, and successful release of the owned token handle. These facts
are prerequisites, not capture consent or a physical-display claim. The borrowed
window-station handle is never closed. No linked/impersonation token is used.

The current manager remains Linux-only until the native environment receipt and
actual staged Windows manager/viewer acceptance pass. An ineligible CI account
is reported; the probe never changes identity or security configuration.

Microsoft references:
- https://learn.microsoft.com/en-us/windows/win32/api/securitybaseapi/nf-securitybaseapi-gettokeninformation
- https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-getprocesswindowstation
- https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-getuserobjectinformationw
