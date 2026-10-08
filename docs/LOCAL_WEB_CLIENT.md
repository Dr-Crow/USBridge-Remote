# Local web client destination

General Settings now includes a local web client URL. It takes effect on the
Status panel's open/copy actions immediately after a successful save; no engine
restart is required. This changes only that destination, not other preferences.

In local runtime network mode, an unset URL displays **Configure local web
client**. Clicking it opens General Settings. It never falls back to
web.usbridge.io. An invalid saved URL also has no public fallback.

Accepted destinations use a literal private, link-local or loopback IP and
HTTPS. HTTP is allowed only on loopback for development. Credentials, query
parameters and fragments are rejected. Hostname-based local deployments remain
a future configuration feature; the current validation does not resolve DNS.

The local web server and the agent's HTTPS endpoint still need certificates
trusted by the browser. Saving this URL does not provision certificates,
configure signaling/ICE, bypass certificate errors, or prove a working stream.
The source-built web bundle and self-host server are separate deployment steps.

The YAML key is `local_web_client_url`. In explicitly online legacy mode with
no configured override, the existing vendor web link remains available.

## Dialog layout and save validation

The settings body scrolls on smaller canvases, while Save remains in a fixed
footer. Wrapped paragraphs receive an initial width before height-dependent
layout. Software rendering exposed clipped lower controls in the earlier form;
the updated layout was reviewed at 1000x720 and 640x480. Automated geometry checks
keep Save within bounds down to a 480x360 canvas. These are Fyne software-driver
checks, not native Windows desktop rendering acceptance.

Tests cover a valid Save button action, trimmed URLs, invalid public URLs that
never reach persistence, failed persistence leaving the current URL unchanged,
clearing the URL without a public fallback, and reopening with saved rather than
unsaved edits. An unavailable engine now produces an error instead of silently
accepting a Save action.
