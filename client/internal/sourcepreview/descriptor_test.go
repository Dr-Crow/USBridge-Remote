package sourcepreview

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var testNow = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

func validWire(t *testing.T) map[string]any {
	t.Helper()
	return map[string]any{
		"schema_version": 1, "profile": Profile, "session_id": "session_0123456789abcdef",
		"rtsp_url": "rtspenc://127.0.0.1:45678", "key_b64": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0xa5}, 16)),
		"key_id": uint32(0xfedcba98), "width": 640, "height": 360, "fps": 30, "bitrate_kbps": 1000,
		"expires_at": testNow.Add(30 * time.Second).Format(time.RFC3339Nano),
	}
}
func encoded(t *testing.T, w map[string]any) []byte {
	t.Helper()
	b, e := json.Marshal(w)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestDecodeAndConsume(t *testing.T) {
	d, err := Decode(encoded(t, validWire(t)), testNow)
	if err != nil {
		t.Fatal(err)
	}
	if d.SessionID() != "session_0123456789abcdef" || !d.ExpiresAt().Equal(testNow.Add(30*time.Second)) {
		t.Fatal("identity/expiry mismatch")
	}
	c, err := d.Consume(testNow)
	if err != nil {
		t.Fatal(err)
	}
	if c.KeyID != 0xfedcba98 || c.Key[0] != 0xa5 || c.Width != 640 || c.Height != 360 || c.FPS != 30 || c.BitrateKbps != 1000 {
		t.Fatal("native mapping mismatch")
	}
	if _, err = d.Consume(testNow); err == nil {
		t.Fatal("descriptor replay accepted")
	}
	for _, b := range d.connection.Key {
		if b != 0 {
			t.Fatal("descriptor retained consumed key")
		}
	}
}
func TestConcurrentSingleUse(t *testing.T) {
	d, err := Decode(encoded(t, validWire(t)), testNow)
	if err != nil {
		t.Fatal(err)
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			if _, err := d.Consume(testNow); err == nil {
				successes.Add(1)
			}
		})
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatal("descriptor not single-use")
	}
}
func TestRejectMalformedDescriptors(t *testing.T) {
	cases := map[string]func(map[string]any){
		"version":         func(w map[string]any) { w["schema_version"] = 2 },
		"profile":         func(w map[string]any) { w["profile"] = "stock" },
		"key length":      func(w map[string]any) { w["key_b64"] = "AA==" },
		"key encoding":    func(w map[string]any) { w["key_b64"] = "secret-do-not-log" },
		"key id overflow": func(w map[string]any) { w["key_id"] = 4294967296 },
		"null id":         func(w map[string]any) { w["key_id"] = nil },
		"missing id":      func(w map[string]any) { delete(w, "key_id") },
		"unknown":         func(w map[string]any) { w["secret-do-not-log"] = "secret-do-not-log" },
		"case alias":      func(w map[string]any) { w["PROFILE"] = Profile },
		"expired":         func(w map[string]any) { w["expires_at"] = testNow.Format(time.RFC3339Nano) },
		"too long":        func(w map[string]any) { w["expires_at"] = testNow.Add(31 * time.Second).Format(time.RFC3339Nano) },
		"width":           func(w map[string]any) { w["width"] = 642 },
		"odd width":       func(w map[string]any) { w["width"] = 639 },
		"height":          func(w map[string]any) { w["height"] = 362 },
		"fps":             func(w map[string]any) { w["fps"] = 31 },
		"bitrate":         func(w map[string]any) { w["bitrate_kbps"] = 0 },
		"unsafe id":       func(w map[string]any) { w["session_id"] = "session\n0123456789" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			w := validWire(t)
			change(w)
			_, err := Decode(encoded(t, w), testNow)
			if err == nil {
				t.Fatal("accepted invalid descriptor")
			}
			if strings.Contains(err.Error(), "secret-do-not-log") {
				t.Fatal("error leaked input")
			}
		})
	}
	base := encoded(t, validWire(t))
	for _, data := range [][]byte{nil, []byte("null"), []byte("[]"), bytes.Repeat([]byte{' '}, MaxDescriptorBytes+1), append(append([]byte{}, base...), base...), bytes.Replace(base, []byte(`"width":640`), []byte(`"width":640,"width":640`), 1)} {
		if _, err := Decode(data, testNow); err == nil {
			t.Fatal("accepted invalid object")
		}
	}
}

func TestRejectJSONFieldAliases(t *testing.T) {
	for field, value := range validWire(t) {
		aliases := []string{strings.ToUpper(field)}
		// encoding/json also folds long s and Kelvin sign to ASCII s and k.
		if alias := strings.NewReplacer("s", "ſ", "k", "K").Replace(field); alias != field {
			aliases = append(aliases, alias)
		}
		for _, alias := range aliases {
			for _, replace := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/replace=%t", alias, replace), func(t *testing.T) {
					w := validWire(t)
					if replace {
						delete(w, field)
					}
					w[alias] = value
					d, err := Decode(encoded(t, w), testNow)
					if d != nil {
						d.Destroy()
						t.Fatal("accepted JSON field alias")
					}
					if err == nil || err.Error() != "invalid source preview descriptor" {
						t.Fatal("missing or non-generic wire error")
					}
				})
			}
		}
	}
}

func TestRejectNonCanonicalBase64Keys(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0xa5}, 16))
	cases := map[string]string{
		"leading CR":       "\r" + key,
		"leading LF":       "\n" + key,
		"embedded CR":      key[:8] + "\r" + key[8:],
		"embedded LF":      key[:8] + "\n" + key[8:],
		"embedded CRLF":    key[:8] + "\r\n" + key[8:],
		"padding CRLF":     key[:len(key)-1] + "\r\n" + key[len(key)-1:],
		"trailing CR":      key + "\r",
		"trailing LF":      key + "\n",
		"space":            key[:8] + " " + key[8:],
		"tab":              key[:8] + "\t" + key[8:],
		"missing padding":  strings.TrimRight(key, "="),
		"extra padding":    key + "=",
		"nonzero pad bits": key[:len(key)-3] + "R==",
		"URL alphabet":     base64.URLEncoding.EncodeToString(bytes.Repeat([]byte{0xff}, 16)),
	}
	for name, key := range cases {
		t.Run(name, func(t *testing.T) {
			w := validWire(t)
			w["key_b64"] = key
			d, err := Decode(encoded(t, w), testNow)
			if d != nil {
				d.Destroy()
				t.Fatal("accepted non-canonical base64 key")
			}
			if err == nil || err.Error() != "invalid source preview descriptor" {
				t.Fatal("missing or non-generic wire error")
			}
		})
	}
}

func TestDecodeCanonicalBase64Keys(t *testing.T) {
	for _, keyByte := range []byte{0x00, 0xa5, 0xfb, 0xff} {
		t.Run(fmt.Sprintf("%02x", keyByte), func(t *testing.T) {
			key := bytes.Repeat([]byte{keyByte}, 16)
			w := validWire(t)
			w["key_b64"] = base64.StdEncoding.EncodeToString(key)
			w["key_id"] = 0
			d, err := Decode(encoded(t, w), testNow)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Destroy()
			c, err := d.Consume(testNow)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(c.Key[:], key) || c.KeyID != 0 {
				t.Fatal("canonical key or zero key ID changed")
			}
		})
	}
}

func TestOnlyEncryptedLoopbackEndpoint(t *testing.T) {
	for _, url := range []string{"rtsp://127.0.0.1:1234", "rtspenc://localhost:1234", "rtspenc://[::1]:1234", "rtspenc://0.0.0.0:1234", "rtspenc://192.168.1.1:1234", "rtspenc://127.0.0.1:0", "rtspenc://127.0.0.1:65536", "rtspenc://127.0.0.1:1234/", "rtspenc://127.0.0.1:1234?key=x", "rtspenc://127.0.0.1:1234#x", "rtspenc://user@127.0.0.1:1234", "rtspenc://127.0.0.1:01234"} {
		if ValidateRTSPURL(url) == nil {
			t.Errorf("accepted %s", url)
		}
	}
	if err := ValidateRTSPURL("rtspenc://127.0.0.1:65535"); err != nil {
		t.Fatal(err)
	}
}
func TestSecretRedactionAndDestruction(t *testing.T) {
	d, err := Decode(encoded(t, validWire(t)), testNow)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []any{d, d.connection} {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			s := fmt.Sprintf(format, v)
			if !strings.Contains(s, "redacted") || strings.Contains(s, "165") {
				t.Fatal("format leaked secret")
			}
		}
		if _, err := json.Marshal(v); err == nil {
			t.Fatal("secret serialized")
		}
	}
	d.Destroy()
	if _, err := d.Consume(testNow); err == nil {
		t.Fatal("destroyed key accepted")
	}
	d, err = Decode(encoded(t, validWire(t)), testNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.Consume(testNow.Add(31 * time.Second)); err == nil {
		t.Fatal("expired key accepted")
	}
}

func FuzzDecodeNeverPanicsOrLeaks(f *testing.F) {
	f.Add([]byte(`{"schema_version":1}`))
	f.Add([]byte(`{"key_b64":"never-echo-this"}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := Decode(data, testNow)
		if err != nil {
			if err.Error() != "invalid source preview descriptor" {
				t.Fatal("non-generic wire error")
			}
			return
		}
		defer d.Destroy()
		c, err := d.Consume(testNow)
		if err != nil {
			t.Fatal(err)
		}
		if ValidateRTSPURL(c.RTSPURL) != nil || c.Width > 640 || c.Height > 360 || c.FPS > 30 {
			t.Fatal("invalid native configuration")
		}
		if _, err = d.Consume(testNow); err == nil {
			t.Fatal("replay")
		}
	})
}

func TestDefaultPreviewProfile(t *testing.T) {
	w := validWire(t)
	w["width"], w["height"] = DefaultWidth, DefaultHeight
	d, err := Decode(encoded(t, w), testNow)
	if err != nil {
		t.Fatal(err)
	}
	c, err := d.Consume(testNow)
	if err != nil {
		t.Fatal(err)
	}
	if c.Width != 128 || c.Height != 72 {
		t.Fatal("unexpected default preview profile")
	}
}
