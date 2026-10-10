# Local source preview implementation (CI experiment)

The agent implementation starts from published, fully accepted fd5788ac809b89fbf84318a9b888d6b728e42bf8, with the separate broker and Go1.26.9 CI updates as its publication base. It does not modify that candidate or its downloadable artifacts. The later broker correction and Go security patch were accepted as f68d34adbc8a6fe2cf4184375280f0e4ffe73458 with all six hosted jobs green; that supplied-session package remains distinct from this pending GUI experiment.

The agent adds a distinct Settings → Source preview action, a one-shot same-user Linux session manager and a hash-verified native viewer subprocess. It does not register a new network/admin-socket endpoint, change saved stock backend selection, or claim first-time Moonlight pairing support.

The local dialog selects the component directory and trusted manifest checksum, FFmpeg executable and exact X11 display. Explicit capture approval is consumed once. The default profile is the validated top-left 128×72 transport region, 30fps, 30seconds, H264420, synthetic silence and no input. An explicitly experimental 640×360 selection remains a development target, not yet accepted for representative desktop content. Existing all-IDR lossless mode can exceed bounded packetization limits. The next source bounded-video pin must be verified before this is offered as a usable desktop preview.

The manager generates a fresh 16-byte key, key ID and session ID. A source-preview-viewer component with profile source-preview-v1 must be in the same hash-pinned manifest. Its only argument is --source-preview-stdin. The bounded JSON descriptor is sent through private stdin, which remains open as the lease. The viewer receives schema_version/profile/session_id/rtsp_url/key_b64/key_id/width/height/fps/bitrate_kbps/expires_at. Keys are not saved, shown, logged or put in argv/environment. Typed stdout events ready, first_frame and stopped carry only version/session identity and a fixed stopped reason. The UI says viewing only after a decoded image is submitted to the native canvas. The event alone does not prove presented pixels; the independent X11 pixel gate must pass.

On stop/close/deadline/child failure, the manager joins the viewer before the source so encrypted client teardown can finish. Concurrent startup is reserved, startup cancellation is propagated, fresh launches do not reuse prior session material, and callbacks from stopped sessions cannot alter another session.

Current validation:
- Manager unit/race tests passed 30 repetitions.
- Actual private-pipe helper process lifecycle/strict duplicate and unknown-field rejection/missing-terminal tests passed 10 race repetitions.
- Agent UI and manager package race tests and vet passed.
- Full agent race/vet passed; the cloud-only root-ownership fixture exclusion remains explicit.
- Actual native viewer build/rendering and GUI interaction acceptance are still pending. The dedicated client renderer is included under client/cmd/source-preview-viewer; native acceptance is a separate conditional CI gate.

Required acceptance before publication as usable: pinned complete native client build; isolated Xvfb generated-content capture through this actual GUI/controller and real viewer; first rendered frames; no effect before consent; real close/reopen and startup-cancel cleanup; key-free logs; packet/encryption compatibility; representative-content encoder bounds. Neither fake-process tests nor a manager alone meet that bar.
