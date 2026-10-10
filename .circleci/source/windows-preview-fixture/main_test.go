// SPDX-License-Identifier: GPL-3.0-only
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestExactArgvFailClosed(t *testing.T) {
	cases := []struct {
		args []string
		want role
	}{{videoArgs, videoRole}, {audioArgs, audioRole}}
	for _, tt := range cases {
		if got := selectRole(slices.Clone(tt.args)); got != tt.want {
			t.Fatal("exact argv rejected")
		}
		for i := range tt.args {
			mutated := slices.Clone(tt.args)
			mutated[i] += "x"
			if selectRole(mutated) != invalidRole {
				t.Fatalf("modified argument accepted at %d", i)
			}
			removed := append(slices.Clone(tt.args[:i]), tt.args[i+1:]...)
			if selectRole(removed) != invalidRole {
				t.Fatalf("missing argument accepted at %d", i)
			}
			inserted := append(slices.Clone(tt.args[:i]), append([]string{"-y"}, tt.args[i:]...)...)
			if selectRole(inserted) != invalidRole {
				t.Fatalf("extra argument accepted at %d", i)
			}
		}
		if selectRole(append(slices.Clone(tt.args), "-report")) != invalidRole {
			t.Fatal("extra option accepted")
		}
	}
	for _, args := range [][]string{nil, {}, {"-version"}, {"-h"}, {"--help"}, {"-f", "lavfi", "-i", "testsrc"}} {
		if selectRole(args) != invalidRole {
			t.Fatalf("unknown argv accepted: %q", args)
		}
	}
	// These independent literals guard the source seam, not a permissive parser.
	wantVideo := "-hide_banner|-nostdin|-v|error|-f|gdigrab|-draw_mouse|0|-offset_x|0|-offset_y|0|-video_size|128x72|-framerate|30|-i|desktop|-an|-pix_fmt|yuv420p|-c:v|libx264|-qp|0|-preset|ultrafast|-tune|zerolatency|-x264-params|aud=1:repeat-headers=1:keyint=1:min-keyint=1:scenecut=0|-threads|1|-f|h264|pipe:1"
	wantAudio := "-hide_banner|-nostdin|-v|error|-f|s16le|-ar|48000|-ac|2|-i|pipe:0|-vn|-c:a|libopus|-b:a|128k|-application|lowdelay|-frame_duration|5|-vbr|off|-mapping_family|0|-f|ogg|-page_duration|5000|-flush_packets|1|pipe:1"
	if strings.Join(videoArgs, "|") != wantVideo || strings.Join(audioArgs, "|") != wantAudio {
		t.Fatal("frozen source argv drift")
	}
}

func testFrame(color byte) []byte {
	var b []byte
	for _, typ := range []byte{9, 7, 8, 6, 5} {
		b = append(b, 0, 0, 0, 1, typ, color)
	}
	return b
}
func testOgg() []byte {
	var stream []byte
	for i := 0; i < audioPageCount+2; i++ {
		payload := make([]byte, 80)
		payload[0] = 0xec
		var granule uint64
		flags := byte(0)
		if i == 0 {
			payload = []byte{'O', 'p', 'u', 's', 'H', 'e', 'a', 'd', 1, 2, 120, 0, 128, 187, 0, 0, 0, 0, 0}
			flags = 2
		} else if i == 1 {
			payload = append([]byte("OpusTags"), make([]byte, 8)...)
		} else {
			granule = uint64(i-1) * 240
			if i == audioPageCount+1 {
				flags = 4
				granule = pcmSamples + 120
			}
		}
		page := make([]byte, 28+len(payload))
		copy(page, "OggS")
		page[5] = flags
		binary.LittleEndian.PutUint64(page[6:], granule)
		binary.LittleEndian.PutUint32(page[14:], 17)
		binary.LittleEndian.PutUint32(page[18:], uint32(i))
		page[26] = 1
		page[27] = byte(len(payload))
		copy(page[28:], payload)
		binary.LittleEndian.PutUint32(page[22:], oggChecksum(page))
		stream = append(stream, page...)
	}
	return stream
}
func hash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func makeTestAssets(t *testing.T, dir string) (assets, assetPins) {
	t.Helper()
	blue, orange, silence := testFrame(17), testFrame(33), testOgg()
	for name, data := range map[string][]byte{blueName: blue, orangeName: orange, silenceName: silence} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	pins := assetPins{hash(blue), hash(orange), hash(silence)}
	a, err := loadAssets(dir, pins)
	if err != nil {
		t.Fatal(err)
	}
	return a, pins
}

func TestAssetHashSizeTypeAndSymlinkRejection(t *testing.T) {
	dir := t.TempDir()
	_, pins := makeTestAssets(t, dir)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, pin := range []string{"", "00", strings.Repeat("z", 64), strings.Repeat("0", 64)} {
		if _, err = loadAsset(root, blueName, pin, maxVideoBytes); err == nil {
			t.Fatal("invalid/missing pin accepted")
		}
	}
	if _, err = loadAsset(root, "missing", pins.blue, maxVideoBytes); err == nil {
		t.Fatal("missing asset accepted")
	}
	if err = os.Mkdir(filepath.Join(dir, "directory"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = loadAsset(root, "directory", pins.blue, maxVideoBytes); err == nil {
		t.Fatal("directory accepted")
	}
	for _, size := range []int{0, maxVideoBytes + 1} {
		data := make([]byte, size)
		os.WriteFile(filepath.Join(dir, "size"), data, 0600)
		if _, err = loadAsset(root, "size", hash(data), maxVideoBytes); err == nil {
			t.Fatal("out-of-bounds size accepted")
		}
	}
	data := testFrame(17)
	os.WriteFile(filepath.Join(dir, "changed"), append(data, 1), 0600)
	if _, err = loadAsset(root, "changed", pins.blue, maxVideoBytes); err == nil {
		t.Fatal("modified bytes accepted")
	}
	t.Run("symlink", func(t *testing.T) {
		if err := os.Symlink(blueName, filepath.Join(dir, "link")); err != nil {
			t.Skip("platform does not allow unprivileged symlink creation")
		}
		if _, err := loadAsset(root, "link", pins.blue, maxVideoBytes); err == nil {
			t.Fatal("symlink accepted")
		}
	})
	// Every pin/asset must be valid before either role can emit anything.
	if _, err = loadAssets(dir, assetPins{pins.blue, pins.orange, ""}); err == nil {
		t.Fatal("unverified audio accepted")
	}
}

func TestAnnexBBoundaries(t *testing.T) {
	valid := testFrame(17)
	if validateFrame(valid) != nil {
		t.Fatal("valid frame rejected")
	}
	mutations := [][]byte{nil, valid[2:], append([]byte{1}, valid...), append(slices.Clone(valid), valid...), append(slices.Clone(valid), 0, 0, 1), {0, 0, 1, 9}}
	notIDR := slices.Clone(valid)
	notIDR[len(notIDR)-2] = 1
	mutations = append(mutations, notIDR)
	forbidden := slices.Clone(valid)
	forbidden[4] |= 0x80
	mutations = append(mutations, forbidden)
	for i, data := range mutations {
		if validateFrame(data) == nil {
			t.Fatalf("malformed Annex-B accepted %d", i)
		}
	}
}

func TestOggStructureAndChecksum(t *testing.T) {
	data := testOgg()
	pages, err := parseOgg(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != audioPageCount+2 {
		t.Fatal("wrong page count")
	}
	// Every header field and payload byte is covered by the CRC, and a valid
	// CRC alone is not sufficient for an invalid duration, granule, or channel.
	for _, offset := range []int{0, 4, 5, 6, 14, 18, 22, 26, 27, 28, len(data) - 1} {
		mutation := slices.Clone(data)
		mutation[offset] ^= 1
		if _, err := parseOgg(mutation); err == nil {
			t.Fatalf("corruption accepted at %d", offset)
		}
	}
	for _, index := range []int{0, 2, audioPageCount + 1} {
		copyData := slices.Clone(data)
		copyPages, _ := parseOgg(copyData)
		p := copyPages[index]
		if index == 0 {
			p[37] = 1
		} else if index == 2 {
			p[28] = 0xf4
		} else {
			p[5] = 0
		}
		binary.LittleEndian.PutUint32(p[22:], oggChecksum(p))
		if _, err := parseOgg(copyData); err == nil {
			t.Fatal("semantically invalid Ogg accepted")
		}
	}
	for _, mutation := range [][]byte{data[:len(data)-1], append(slices.Clone(data), data...), data[:len(pages[0])]} {
		if _, err := parseOgg(mutation); err == nil {
			t.Fatal("truncated or chained Ogg accepted")
		}
	}
}

type fakeClock struct {
	start  time.Time
	waits  []time.Duration
	failAt int
}

func (c *fakeClock) Now() time.Time { return c.start }
func (c *fakeClock) Wait(ctx context.Context, at time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.failAt > 0 && len(c.waits) == c.failAt {
		return context.Canceled
	}
	c.waits = append(c.waits, at.Sub(c.start))
	return nil
}

type zeroReader struct{ count int }

func (r *zeroReader) Read(p []byte) (int, error) { clear(p); r.count += len(p); return len(p), nil }

type checkingWriter struct {
	clock  *fakeClock
	blocks [][]byte
	failAt int
}

func (w *checkingWriter) Write(p []byte) (int, error) {
	if w.failAt > 0 && len(w.blocks) == w.failAt {
		return 0, errors.New("broken pipe")
	}
	w.blocks = append(w.blocks, slices.Clone(p))
	return len(p), nil
}
func TestPacedRepeatedVideo(t *testing.T) {
	a, _ := makeTestAssets(t, t.TempDir())
	clock := &fakeClock{start: time.Unix(0, 0)}
	out := &checkingWriter{}
	if err := emit(context.Background(), videoRole, a, nil, out, clock); err != nil {
		t.Fatal(err)
	}
	if len(out.blocks) != 900 || len(clock.waits) != 900 {
		t.Fatal("incorrect video bound")
	}
	for i, b := range out.blocks {
		expected := a.blue
		if (i/30)%2 == 1 {
			expected = a.orange
		}
		if !bytes.Equal(b, expected) || clock.waits[i] != time.Duration(i)*time.Second/30 {
			t.Fatalf("frame/pacing mismatch %d", i)
		}
	}
	if clock.waits[899] >= 30*time.Second {
		t.Fatal("video exceeds lease")
	}
}
func TestPacedOggAndBoundedPCM(t *testing.T) {
	a, _ := makeTestAssets(t, t.TempDir())
	clock := &fakeClock{start: time.Unix(0, 0)}
	out := &checkingWriter{}
	input := &zeroReader{}
	if err := emit(context.Background(), audioRole, a, input, out, clock); err != nil {
		t.Fatal(err)
	}
	if len(out.blocks) != audioPageCount+2 || len(clock.waits) != audioPageCount || input.count != pcmBytes {
		t.Fatal("incorrect audio/PCM bound")
	}
	for i, d := range clock.waits {
		if d != time.Duration(i)*5*time.Millisecond {
			t.Fatal("audio pacing mismatch")
		}
	}
	if clock.waits[len(clock.waits)-1] >= 30*time.Second {
		t.Fatal("audio exceeds lease")
	}
	for i, b := range out.blocks {
		if !bytes.Equal(b, a.pages[i]) {
			t.Fatal("Ogg bytes changed")
		}
	}
	// Even with an empty input, both headers are emitted immediately and no
	// audio data page is emitted without its bounded matching PCM block.
	empty := &checkingWriter{}
	clock = &fakeClock{start: time.Unix(0, 0)}
	if err := emit(context.Background(), audioRole, a, bytes.NewReader(nil), empty, clock); err == nil || len(empty.blocks) != 2 {
		t.Fatal("short PCM did not fail closed after headers")
	}
}
func TestImmediateBrokenPipeAndCancellation(t *testing.T) {
	a, _ := makeTestAssets(t, t.TempDir())
	for _, r := range []role{videoRole, audioRole} {
		clock := &fakeClock{start: time.Unix(0, 0)}
		out := &checkingWriter{failAt: 3}
		if err := emit(context.Background(), r, a, &zeroReader{}, out, clock); err == nil || len(out.blocks) != 3 {
			t.Fatal("broken output did not stop")
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := emit(ctx, r, a, &zeroReader{}, io.Discard, clock); !errors.Is(err, context.Canceled) {
			t.Fatal("cancellation not honored")
		}
	}
}
func TestWallClockPacing(t *testing.T) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := (wallClock{}).Wait(ctx, start.Add(30*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 25*time.Millisecond {
		t.Fatal("wait returned too early")
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	if err := (wallClock{}).Wait(canceled, time.Now().Add(time.Hour)); err != context.Canceled {
		t.Fatal("wait ignored cancellation")
	}
}

// No runtime extension points, environment-based configuration, subprocesses,
// codec libraries, desktop APIs, or network dependencies can be added quietly.
func TestRuntimeImportAndAPIAllowlist(t *testing.T) {
	allowed := map[string]bool{"bytes": true, "context": true, "crypto/sha256": true, "encoding/binary": true, "encoding/hex": true, "errors": true, "fmt": true, "io": true, "os": true, "path/filepath": true, "slices": true, "time": true}
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range sources {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range file.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if !allowed[path] {
				t.Fatalf("unapproved runtime import %s", path)
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			if ident.Name == "os" && slices.Contains([]string{"StartProcess", "Getenv", "LookupEnv", "Environ", "ExpandEnv"}, sel.Sel.Name) {
				t.Errorf("unapproved runtime API os.%s", sel.Sel.Name)
			}
			return true
		})
	}
}

func TestFixtureHelperProcess(t *testing.T) {
	if os.Getenv("FIXTURE_TEST_CHILD") != "1" {
		return
	}
	videoBlueSHA256 = os.Getenv("FIXTURE_TEST_BLUE")
	videoOrangeSHA256 = os.Getenv("FIXTURE_TEST_ORANGE")
	audioSilenceSHA256 = os.Getenv("FIXTURE_TEST_SILENCE")
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{os.Args[0]}, os.Args[i+1:]...)
			break
		}
	}
	main()
	os.Exit(0)
}
func TestChildBrokenPipe(t *testing.T) {
	dir := t.TempDir()
	_, pins := makeTestAssets(t, dir)
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(dir, "fixture-test-child"+filepath.Ext(source))
	binaryData, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(child, binaryData, 0700); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{videoArgs, audioArgs} {
		t.Run(strconv.Itoa(int(selectRole(args))), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, child, append([]string{"-test.run=^TestFixtureHelperProcess$", "--"}, args...)...)
			cmd.Env = append(os.Environ(), "FIXTURE_TEST_CHILD=1", "FIXTURE_TEST_BLUE="+pins.blue, "FIXTURE_TEST_ORANGE="+pins.orange, "FIXTURE_TEST_SILENCE="+pins.silence)
			cmd.Stdin = io.LimitReader(&zeroReader{}, pcmBytes)
			read, write, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer read.Close()
			var stderr bytes.Buffer
			cmd.Stdout = write
			cmd.Stderr = &stderr
			if err = cmd.Start(); err != nil {
				write.Close()
				t.Fatal(err)
			}
			write.Close()
			one := make([]byte, 1)
			if _, err = io.ReadFull(read, one); err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			read.Close()
			err = cmd.Wait()
			if err == nil || ctx.Err() != nil || time.Since(start) > time.Second {
				t.Fatalf("child failed to exit promptly on broken output: %v", err)
			}
			if got := stderr.String(); got != "" && got != "synthetic fixture failed\n" {
				t.Fatalf("nongeneric stderr: %q", got)
			}
		})
	}
}
