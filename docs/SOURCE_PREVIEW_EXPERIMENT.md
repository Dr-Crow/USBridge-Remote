# Local source preview implementation (CI experiment)

The agent implementation starts from published, fully accepted fd5788ac809b89fbf84318a9b888d6b728e42bf8, with the separate broker and Go1.26.9 CI updates as its publication base. It does not modify that candidate or its downloadable artifacts. The later broker correction and Go security patch were accepted as f68d34adbc8a6fe2cf4184375280f0e4ffe73458 with all six hosted jobs green; that supplied-session package remains distinct from this pending GUI experiment.

The agent adds a distinct Settings → Source preview action, a one-shot same-user Linux session manager and a hash-verified native viewer subprocess. It does not register a new network/admin-socket endpoint, change saved stock backend selection, or claim first-time Moonlight pairing support.

The local dialog selects the component directory and trusted manifest checksum, FFmpeg executable and exact X11 display. Explicit capture approval is consumed once. The default profile requests the validated top-left 128×72 transport region, 30fps, 30seconds, H264420, synthetic silence and no input. An explicitly experimental 640×360 selection remains a development target, not yet accepted for representative desktop content. Existing all-IDR lossless mode can exceed bounded packetization limits. The next source bounded-video pin must be verified before this is offered as a usable desktop preview.

The currently packaged source pin remains `2e07af3484369bc68bc8091d5a04969867eff0f9`. Later source testing exposed intermittent FFmpeg output-rate overproduction in that implementation. The pixel/lifecycle tests below establish actual presentation and cleanup, not stable delivered 30fps cadence or desktop performance. A later source fix requires a separately verified package update; its results must not be attributed to this older package.

The same older source also has an observed intermittent listener retirement race:
`TestLaunchListenerTCPFlowAndCancellation` failed at line 61 during the first
`55461e5` CI run and reproduced in a separate repeated local run. A same-commit
retry does not remove this known startup limitation. The later component fix is
not included in this package; no assertion is excluded to hide the failure.

The manager generates a fresh 16-byte key, key ID and session ID. A source-preview-viewer component with profile source-preview-v1 must be in the same hash-pinned manifest. Its only argument is --source-preview-stdin. The bounded JSON descriptor is sent through private stdin, which remains open as the lease. The viewer receives schema_version/profile/session_id/rtsp_url/key_b64/key_id/width/height/fps/bitrate_kbps/expires_at. Keys are not saved, shown, logged or put in argv/environment. Typed stdout events ready, first_frame and stopped carry only version/session identity and a fixed stopped reason. The UI says viewing only after a decoded image is submitted to the native canvas. The event alone does not prove presented pixels; the independent X11 pixel gate must pass.

On stop/close/deadline/child failure, the manager joins the viewer before the source so encrypted client teardown can finish. Concurrent startup is reserved, startup cancellation is propagated, fresh launches do not reuse prior session material, and callbacks from stopped sessions cannot alter another session.

Current validation:
- Manager unit/race tests passed 30 repetitions.
- Actual private-pipe helper process lifecycle/strict duplicate and unknown-field rejection/missing-terminal tests passed 10 race repetitions.
- Agent UI and manager package race tests and vet passed.
- Full agent race/vet passed; the cloud-only root-ownership fixture exclusion remains explicit.
- Actual native viewer rendering and the native settings/dialog gate passed at `8bf63eceaaa648f5d5b08c45ee6d2cbfc853d40d`. Its three fresh preview sessions produced 29 independent probes, each with 144/144 matching pixels, and joined their media children on Stop, dialog Close and parent close-to-tray. The dialog fixture uses an offline parent-engine facade.
- The separate ordinary CLI/App.New gate at that commit failed after consent/invalid-manifest checks (completed stage 4), before verified preview presentation. That production-engine path remains pending; the dialog result alone does not close it. See `SOURCE_PREVIEW_ENGINE_CI.md` for the unchanged isolation and staging limitation.

Required acceptance before publication as usable: pinned complete native client build; isolated Xvfb generated-content capture through this actual GUI/controller and real viewer; first rendered frames; no effect before consent; real close/reopen and startup-cancel cleanup; key-free logs; packet/encryption compatibility; representative-content encoder bounds. Neither fake-process tests nor a manager alone meet that bar.
