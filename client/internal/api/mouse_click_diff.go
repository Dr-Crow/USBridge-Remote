package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"time"
)

// clickSettleDelay is how long to wait after the click actually lands
// (the device's own response, forwarded through unmodified) before
// grabbing the "after" frame -- a UI reacting to a click (a menu opening,
// a window raising) isn't necessarily done rendering the instant the
// input event was accepted. Short enough not to make every click feel
// laggy; matches the general "wait ~150-300ms for the UI to react" advice
// already baked into this MCP surface's own tool descriptions.
const clickSettleDelay = 200 * time.Millisecond

// tryLocalMouseClickDiff intercepts a tools/call "mouse.action" request
// whose action is click_at/double_click_at and computes the before/after
// screen_changed_pct/screen_visibly_changed fields locally, from the video
// decode pipeline's own frames (see live_frame.go), instead of trusting
// the device's own answer.
//
// Why: click_at/double_click_at's confirmation diff is meant to answer
// "did this click visibly do anything" -- but the Agent computes that by
// capturing its OWN before/after screenshots (mcp_mouse_absolute.go's
// currentScreenPNG), which depends on the same target-OS screen capture
// screen.get_image does. Confirmed live against a KDE/Wayland Agent: that
// capture is unreliable there (was hanging on an unanswerable portal
// dialog; routed around that, it returns a blank black frame instead, since
// KWin blocks plain X11 capture over XWayland) -- a blank-vs-blank diff
// always reads "no change", silently lying to the caller regardless of
// what actually happened on screen. The Client is already displaying the
// real picture whenever a session is streaming, so there's no reason to
// trust the device's own (possibly broken) capture for this at all.
//
// The click itself still goes to the device unchanged -- only the
// diff fields in its response get overwritten, and only when both a
// before and an after live frame were actually available (session
// streaming, UI actively rendering). Falls through to the device's own
// answer untouched otherwise, exactly as if this didn't exist.
func tryLocalMouseClickDiff(client *USBClient, reqBody []byte) (respBody []byte, handled bool, err error) {
	var env mcpEnvelope
	if jsonErr := json.Unmarshal(reqBody, &env); jsonErr != nil || env.Method != "tools/call" {
		return nil, false, nil
	}
	var call mcpToolCallParams
	if jsonErr := json.Unmarshal(env.Params, &call); jsonErr != nil || call.Name != "mouse.action" {
		return nil, false, nil
	}
	var args struct {
		Action string `json:"action"`
	}
	if jsonErr := json.Unmarshal(call.Arguments, &args); jsonErr != nil {
		return nil, false, nil
	}
	if args.Action != "click_at" && args.Action != "double_click_at" {
		return nil, false, nil
	}

	beforePNG, ok := RequestLiveFrame(liveFrameWaitTimeout)
	if !ok {
		return nil, false, nil // no active stream -- let the device's own diff (or lack of one) stand
	}

	resp, postErr := client.PostRawWithTimeout("/api/mcp", reqBody, mcpProxyTimeout)
	if postErr != nil {
		return nil, true, postErr
	}

	time.Sleep(clickSettleDelay)
	afterPNG, ok := RequestLiveFrame(liveFrameWaitTimeout)
	if !ok {
		return resp, true, nil // couldn't get a fresh "after" frame -- pass the device's own answer through
	}

	pct, diffErr := screenChangePercent(beforePNG, afterPNG)
	if diffErr != nil {
		return resp, true, nil
	}

	return overwriteScreenDiffFields(resp, pct), true, nil
}

// overwriteScreenDiffFields replaces screen_changed_pct/screen_visibly_changed
// in a mouse.action click_at/double_click_at response's JSON text content
// block with locally-computed values -- best-effort: any parse failure
// just returns resp unchanged rather than erroring a click that already
// succeeded on the device.
func overwriteScreenDiffFields(resp []byte, pct float64) []byte {
	var env mcpEnvelope
	if err := json.Unmarshal(resp, &env); err != nil || len(env.Result) == 0 {
		return resp
	}
	var toolResult mcpToolResult
	if err := json.Unmarshal(env.Result, &toolResult); err != nil {
		return resp
	}
	changed := false
	for i, c := range toolResult.Content {
		if c.Type != "text" {
			continue
		}
		var fields map[string]any
		if err := json.Unmarshal([]byte(c.Text), &fields); err != nil {
			continue
		}
		if _, ok := fields["status"]; !ok {
			continue // not the click-result text block (e.g. ui.type_text has its own shape)
		}
		fields["screen_changed_pct"] = pct
		fields["screen_visibly_changed"] = pct > 0.05
		newText, err := json.Marshal(fields)
		if err != nil {
			continue
		}
		toolResult.Content[i].Text = string(newText)
		changed = true
		break
	}
	if !changed {
		return resp
	}
	resultJSON, err := json.Marshal(toolResult)
	if err != nil {
		return resp
	}
	env.Result = resultJSON
	out, err := json.Marshal(env)
	if err != nil {
		return resp
	}
	return out
}

// screenChangePercent is the Client's own copy of the Agent's pure-Go
// screen diff (agent/internal/api/screen_diff.go) -- same algorithm,
// threshold and 1/16-frame sampling stride, kept in sync by hand since the
// two repos don't share a Go module. See that file's doc comment for the
// full rationale.
func screenChangePercent(beforePNG, afterPNG []byte) (float64, error) {
	before, err := png.Decode(bytes.NewReader(beforePNG))
	if err != nil {
		return 0, fmt.Errorf("decode before: %w", err)
	}
	after, err := png.Decode(bytes.NewReader(afterPNG))
	if err != nil {
		return 0, fmt.Errorf("decode after: %w", err)
	}

	bb := before.Bounds()
	ab := after.Bounds()
	if bb.Dx() != ab.Dx() || bb.Dy() != ab.Dy() {
		return 100, nil
	}
	w, h := bb.Dx(), bb.Dy()
	if w == 0 || h == 0 {
		return 0, fmt.Errorf("empty capture")
	}

	const stride = 4
	const lumaThreshold = 12

	var changedPx, total int
	for y := 0; y < h; y += stride {
		for x := 0; x < w; x += stride {
			r1, g1, b1, _ := before.At(bb.Min.X+x, bb.Min.Y+y).RGBA()
			r2, g2, b2, _ := after.At(ab.Min.X+x, ab.Min.Y+y).RGBA()
			l1 := (299*int(r1>>8) + 587*int(g1>>8) + 114*int(b1>>8)) / 1000
			l2 := (299*int(r2>>8) + 587*int(g2>>8) + 114*int(b2>>8)) / 1000
			diff := l1 - l2
			if diff < 0 {
				diff = -diff
			}
			total++
			if diff > lumaThreshold {
				changedPx++
			}
		}
	}
	if total == 0 {
		return 0, fmt.Errorf("empty sample")
	}
	return float64(changedPx) / float64(total) * 100, nil
}
