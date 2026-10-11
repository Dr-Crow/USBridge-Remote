# Signal observer and admin Serve lifetime

This focused increment adds one engine owner for the diagnostic signal observer
and the local admin server's Serve/Close workers. Normal engine cancellation
unregisters and joins the observer instead of leaving it blocked waiting for a
signal. Registration is sealed before shutdown; an admin listener being created
when cancellation arrives stays owned until acquisition, Close and Serve are
joined under one five-second deadline. Ordinary optional listen failure remains
degraded startup, but partial acquisition and close uncertainty cannot be
mistaken for ordinary unavailability.

Admin Server.Close explicitly retires its acquired listener even if Serve has
not started, preserves substantive errors and is idempotent. Existing socket
location, permission, route authentication and RPC behavior are unchanged.

This is not a full App in-process retirement claim. Admin request handlers and
Tailscale authentication callbacks, clipboard callback leases, global log
watchers, other engine watchdogs and process-relaunch paths still need distinct
ownership evidence. A joined Serve loop alone does not prove those workers
joined. Windows settings/Manager enablement is not changed by this increment.
