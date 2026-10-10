# Experimental source-streamer process boundary

This is a separate, opt-in source-built backend contract. It is not a drop-in
replacement for stock RustShine, does not use vendor runtime rekeying, and does
not enable an agent remote endpoint or OS input/USB passthrough.

## Agent invocation

```
usbridge_agent --source-streamer-mode \
  --source-component-directory /absolute/components \
  --source-manifest-sha256 <trusted-manifest-sha256> \
  --source-state-dir /absolute/experimental-state
```

The component directory uses the existing local-components manifest schema 1,
with component name `source-streamer`, profile `source-streamer-v1`, platform
`linux/amd64`, and an entry plus size/SHA-256 for every packaged file. The agent
requires the manifest pin, stages verified files, and rechecks them before launch.
No vendor download or public fallback occurs. Source builds for other platforms
must not advertise the v1 Linux X11 capabilities unless actually implemented.

Send exactly one newline-terminated JSON object on stdin. Keep stdin open until
the session should end; EOF stops and joins the child. Send keys only through
this pipe, never arguments or persistent configuration. Generate a fresh 16-byte
session key for every launch; the peer must use the same key for encrypted RTSP.

Fields: `schema_version:1`, `owner`, `session_id`, `key_b64`, `key_id`,
`peer_ip:"127.0.0.1"`, distinct `video_port`/`audio_port`, `display` (local X11,
for example `:99`), `capture_consent:true`, absolute trusted `ffmpeg` path,
`width`, `height`, `fps`, `pixel_format` (`yuv420p` or `yuv444p`), `packet_size`,
`audio_mode:"silence"`, and `max_seconds` (1–300). Messages are capped at 64 KiB.
There is deliberately no implicit display or capture consent.

The source executable is called with `--launch-stdin`. Its first stdout event
must be a strict `ready` object with `schema_version:1`, the same `session_id`,
literal loopback `rtsp_address` and `control_address`, and capabilities
`rtsp-encrypted`, `video-x11-h264`, `audio-silence`, `control-enet`. The agent
fails closed if any capability is missing or an unknown capability is claimed.
Readiness means the local listeners are bound; it does not establish that a
client has completed negotiation or received decoded media. Capture starts only
following the source streamer's authenticated encrypted-RTSP PLAY sequence.

The agent returns the typed ready event and a `stopped` event after graceful
completion. Child stderr and arbitrary status text are not forwarded, so a
malformed subprocess cannot cause a launch key to be echoed into agent logs.
Startup is bounded to 10 seconds, session duration to 300 seconds, and shutdown
to a 3-second grace period before forced cancellation.

## Verification and limitations

The supervisor has race-tested process fixtures for start/stop/restart,
manifest/profile checks, secret-free arguments, invalid readiness, non-loopback
listeners, missing capabilities, oversized output, and child startup timeout.
Those fixtures do not establish interoperability with a source implementation.
Real encrypted-RTSP/ENet/media acceptance must be recorded separately against a
specific source commit. Native audio, non-Linux capture, hardware encoders, OS
input, authenticated remote forwarding, and production desktop parity remain
outside this experimental v1 boundary.
