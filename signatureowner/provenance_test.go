// SPDX-License-Identifier: GPL-3.0-only
package signatureowner

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestExactUpstreamBlobsAndNoTransferredNativeClaim(t *testing.T) {
	var p struct {
		SourceCommit string `json:"source_commit"`
		Files        []struct {
			Path, Snapshot string
			GitBlob        string `json:"git_blob"`
			SHA256         string `json:"sha256"`
		} `json:"upstream_files"`
		Native   string `json:"extracted_package_native_execution"`
		Transfer bool   `json:"original_receipt_is_extraction_native_proof"`
	}
	b, err := os.ReadFile("PROVENANCE.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	if p.SourceCommit != "c451d77c6bd24028f7281e69ab8aa29c0ff2d1c8" || p.Native != "not_run" || p.Transfer || len(p.Files) < 10 {
		t.Fatal("provenance/verification boundary changed")
	}
	for _, f := range p.Files {
		b, err := os.ReadFile(f.Snapshot)
		if err != nil {
			t.Fatal(err)
		}
		h := sha1.New()
		fmt.Fprintf(h, "blob %d%c", len(b), 0)
		h.Write(b)
		s := sha256.Sum256(b)
		if hex.EncodeToString(h.Sum(nil)) != f.GitBlob || hex.EncodeToString(s[:]) != f.SHA256 {
			t.Fatalf("source blob changed: %s", f.Path)
		}
	}
}

// Compare token streams of declarations, ignoring only package names/comments/
// formatting. This makes untouched extracted policy/native primitives auditable.
func declaration(t *testing.T, path, name string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, path, b, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		key := fn.Name.Name
		if fn.Recv != nil {
			var receiver ast.Expr = fn.Recv.List[0].Type
			if p, ok := receiver.(*ast.StarExpr); ok {
				receiver = p.X
			}
			key = receiver.(*ast.Ident).Name + "." + key
		}
		if key == name {
			return string(b[fs.Position(d.Pos()).Offset:fs.Position(d.End()).Offset])
		}
	}
	t.Fatalf("missing declaration %s in %s", name, path)
	return ""
}
func tokens(s string) string {
	var out strings.Builder
	var scanner scanner.Scanner
	fs := token.NewFileSet()
	scanner.Init(fs.AddFile("", -1, len(s)), []byte(s), nil, 0)
	for {
		_, kind, literal := scanner.Scan()
		if kind == token.EOF {
			break
		}
		if kind == token.SEMICOLON {
			continue
		}
		fmt.Fprintf(&out, "%s:%q\n", kind, literal)
	}
	return out.String()
}
func sameDeclaration(t *testing.T, source, target, name string, adapt func(string) string) {
	t.Helper()
	want := declaration(t, "testdata/upstream/"+source, name)
	if adapt != nil {
		want = adapt(want)
	}
	want = resourceChanges(want)
	got := declaration(t, target, name)
	if tokens(want) != tokens(got) {
		t.Fatalf("extraction drift in %s (%s -> %s)", name, source, target)
	}
}
func TestUnchangedOwnershipAndRetirementDeclarations(t *testing.T) {
	for _, name := range []string{"signatureOwnerPolicy.admit", "signatureOwnerPolicy.finish", "signatureUniqueInventory", "signatureExactInitialSet", "signatureOwnerPolicy.freezeStartup"} {
		sameDeclaration(t, "signature_owner.go", "signature_owner.go", name, nil)
	}
	sameDeclaration(t, "retirement.go", "retirement.go", "waitRetiredInventory", nil)
	for _, name := range []string{"newJob", "decodeInventory", "child.close", "child.alive", "processPath", "lockFile"} {
		sameDeclaration(t, "winapi_windows.go", "winapi_windows.go", name, nil)
	}
	sameDeclaration(t, "winapi_windows.go", "winapi_windows.go", "privatePipe", func(s string) string { return strings.ReplaceAll(s, `"preview-pipe"`, `"signature-pipe"`) })
	sameDeclaration(t, "winapi_windows.go", "winapi_windows.go", "child.read", func(s string) string { return strings.ReplaceAll(s, "n >= 3", "n >= 2") })
	for _, name := range []string{"signatureSystemDirectory", "prepareSignatureConsole", "signatureConsoleOwner.close", "signatureConsoleOwner.observe", "signatureProcessZero"} {
		sameDeclaration(t, "signature_owner_windows.go", "signature_owner_windows.go", name, nil)
	}
	sameDeclaration(t, "main.go", "api.go", "naturalChildExit", nil)
	sameDeclaration(t, "main.go", "api.go", "fileSHA", nil)
	sameDeclaration(t, "graphics_modules_windows.go", "run_windows.go", "graphicsPath", nil)
}
func TestNativeCollectionOnlyChangesToSharedDeadlineAndResult(t *testing.T) {
	sameDeclaration(t, "signature_owner_windows.go", "signature_owner_windows.go", "signatureConsoleOwner.collect", func(s string) string {
		s = strings.ReplaceAll(s, "collect(j *job, p *child, r *graphicsOSInspection)", "collect(ctx context.Context, j *job, p *child, r *Result)")
		s = strings.ReplaceAll(s, "\ttimeout := time.NewTimer(6 * time.Second)\n\tdefer timeout.Stop()\n", "")
		s = strings.ReplaceAll(s, "waitRetiredInventory(j.pids, map[uint32]bool{p.pid: true, o.policy.host: true}, time.Second)", "waitRetiredInventoryContext(ctx, j.pids, map[uint32]bool{p.pid: true, o.policy.host: true}, time.Second)")
		s = strings.ReplaceAll(s, "p.wait(time.Second)", "p.waitContext(ctx)")
		s = strings.ReplaceAll(s, "p.next(time.Second)", "p.nextContext(ctx)")
		s = strings.ReplaceAll(s, "p.finishProtocol()", "p.finishProtocolContext(ctx)")
		s = strings.ReplaceAll(s, "case <-timeout.C:\n\t\t\treturn raw, failure(\"signature_owner_timeout\")", "case <-ctx.Done():\n return raw,ctx.Err()")
		return s
	})
	sameDeclaration(t, "signature_owner_windows.go", "signature_owner_windows.go", "signatureConsoleOwner.startup", func(s string) string {
		s = strings.ReplaceAll(s, "startup(j *job, p *child, r *graphicsOSInspection)", "startup(ctx context.Context, j *job, p *child, r *Result)")
		s = strings.ReplaceAll(s, "case <-timer.C:\n\t\t\treturn failure(\"signature_startup_timeout\")", "case <-timer.C:\n return failure(\"signature_startup_timeout\")\n case <-ctx.Done():\n return ctx.Err()")
		return s
	})
}
func TestExtractionHasNoGenericViewerOrLaunchSurface(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		text := string(b)
		for _, bad := range []string{"os/exec", "os.Args", "PrintWindow", "EnumWindows", "user32.dll", "gdi32.dll", "signatureConsole bool", "DETACHED_PROCESS", "exec.Command"} {
			if strings.Contains(text, bad) {
				t.Fatalf("generic capability %s in %s", bad, name)
			}
		}
	}
	b, err := os.ReadFile("winapi_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"0x20002", "0x2000D", "0x4 | 0x400 | 0x80000 | 0x08000000", "syscall.FILE_SHARE_READ", "resumed != 1"} {
		if !strings.Contains(string(b), required) {
			t.Fatalf("native invariant missing: %s", required)
		}
	}
}

func TestNativeHandleAndLaunchAdaptationsAreExplicit(t *testing.T) {
	sameDeclaration(t, "signature_owner_windows.go", "signature_owner_windows.go", "signatureAccounting", func(s string) string {
		return strings.Replace(s, "var a signatureJobAccounting", "j.mu.Lock(); defer j.mu.Unlock(); var a signatureJobAccounting; if j.closed.Load() { return a,failure(\"safety_job_closed\") }", 1)
	})
	sameDeclaration(t, "signature_owner_windows.go", "signature_owner_windows.go", "signatureConsoleOwner.beforeResume", func(s string) string {
		needle := "if !j.contains(h) {"
		add := `if o.rootFile==nil { return failure("signature_root_pin_missing") }
   info,e:=os.Stat(path); held,he:=o.rootFile.Stat(); hash,hashErr:=fileSHA(o.rootFile,64<<20)
   if e!=nil || he!=nil || hashErr!=nil || !os.SameFile(info,held) || hash!=o.rootHash { return failure("signature_root_hash_failed") }
   `
		return strings.Replace(s, needle, add+needle, 1)
	})
	sameDeclaration(t, "winapi_windows.go", "winapi_windows.go", "job.close", func(s string) string {
		return strings.Replace(s, "j.closed.Store(true)", "j.mu.Lock(); defer j.mu.Unlock(); j.closed.Store(true)", 1)
	})
	sameDeclaration(t, "winapi_windows.go", "winapi_windows.go", "job.pidsSnapshot", func(s string) string {
		return strings.Replace(s, "var list jobProcessList", "j.mu.Lock(); defer j.mu.Unlock(); var list jobProcessList; if j.closed.Load() { return nil,list,failure(\"safety_job_closed\") }", 1)
	})
	sameDeclaration(t, "winapi_windows.go", "winapi_windows.go", "job.contains", func(s string) string {
		return strings.Replace(s, "var yes int32", "j.mu.Lock(); defer j.mu.Unlock(); if j.closed.Load() { return false }; var yes int32", 1)
	})
	sameDeclaration(t, "winapi_windows.go", "winapi_windows.go", "job.start", func(s string) string {
		a := strings.Index(s, "// CREATE_SUSPENDED +")
		b := strings.Index(s[a:], "\n\te = syscall.CreateProcess") + a
		s = s[:a] + "flags := uint32(0x4 | 0x400 | 0x80000 | 0x08000000)" + s[b:]
		call := "e = syscall.CreateProcess(exe, cmd, nil, nil, true, flags, &block[0], cwd, &si.Startup, &pi)"
		s = strings.Replace(s, call, "j.mu.Lock(); if j.closed.Load() { j.mu.Unlock(); return nil,failure(\"safety_job_closed\") }; "+call+"; j.mu.Unlock()", 1)
		s = strings.Replace(s, "_, _ = syscall.WaitForSingleObject(pi.Process, 3000)", `state,e:=syscall.WaitForSingleObject(pi.Process,3000)
   if e!=nil || state!=syscall.WAIT_OBJECT_0 { j.launchCleanupFailed=true; code="signature_start_cleanup_join_failed" }`, 1)
		return s
	})
}

// Resource-close accounting is an explicit extraction adaptation. Every source
// release now records typed uncertainty; no ignored close may justify success.
func resourceChanges(s string) string {
	replacements := [][2]string{
		{"func newJob()", "func newJob(resources *resourceTracker)"},
		{"&job{handle: syscall.Handle(h)}", "&job{handle: syscall.Handle(h),resources:resources}"},
		{"_ = syscall.CloseHandle(j.handle)", "j.resources.record(\"job_handle\",syscall.CloseHandle(j.handle))"},
		{"_ = p.stdin.Close()", "_ = p.closeStdin()"},
		{"_ = p.stdout.Close()", "p.owner.resources.record(\"stdout\",p.stdout.Close())"},
		{"_ = p.stderr.Close()", "p.owner.resources.record(\"stderr\",p.stderr.Close())"},
		{"_ = syscall.CloseHandle(p.handle)", "p.owner.resources.record(\"root_handle\",syscall.CloseHandle(p.handle))"},
		{"defer syscall.CloseHandle(p.waitHandle)", "defer func(){p.owner.resources.record(\"wait_handle\",syscall.CloseHandle(p.waitHandle))}()"},
		{"func prepareSignatureConsole(root string)", "func prepareSignatureConsole(root string,resources *resourceTracker)"},
		{"&signatureConsoleOwner{file: f, path: final, hash: hash}", "&signatureConsoleOwner{file: f, path: final, hash: hash,resources:resources}"},
		{"return nil, failure(\"signature_host_lock_failed\")", "return nil,errors.Join(failure(\"signature_host_lock_failed\"),err)"},
		{"f.Close()\n\t\treturn nil, failure(\"signature_host_final_path_failed\")", "return nil,closeFailure(failure(\"signature_host_final_path_failed\"),\"console_file\",f.Close())"},
		{"f.Close()\n\t\treturn nil, failure(\"signature_host_hash_failed\")", "return nil,closeFailure(failure(\"signature_host_hash_failed\"),\"console_file\",f.Close())"},
		{"syscall.CloseHandle(o.rootHandle)", "o.resources.record(\"root_observed_handle\",syscall.CloseHandle(o.rootHandle))"},
		{"syscall.CloseHandle(o.handle)", "o.resources.record(\"console_handle\",syscall.CloseHandle(o.handle))"},
		{"o.file.Close()", "o.resources.record(\"console_file\",o.file.Close())"},
		{"return failure(\"signature_member_inspection_failed\")", "return errors.Join(failure(\"signature_member_inspection_failed\"),err)"},
		{"syscall.CloseHandle(h)\n\t\t\treturn err", "o.resources.record(\"inspection_handle\",syscall.CloseHandle(h))\nreturn err"},
		{"syscall.CloseHandle(h)\n\t\treturn \"\", 0, failure(\"owned_process_path_failed\")", "return \"\",0,closeFailure(failure(\"owned_process_path_failed\"),\"inspection_handle\",syscall.CloseHandle(h))"},
		{"f.Close()\n\t\treturn nil, failure(\"component_identity_changed\")", "return nil,closeFailure(failure(\"component_identity_changed\"),\"dependency\",f.Close())"},
		{"defer syscall.CloseHandle(pi.Thread)", "defer func(){j.resources.record(\"launch_thread\",syscall.CloseHandle(pi.Thread))}()"},
		{"_ = syscall.CloseHandle(pi.Process)", "j.resources.record(\"launch_process\",syscall.CloseHandle(pi.Process))"},
		{"syscall.CloseHandle(waitHandle)", "j.resources.record(\"wait_handle\",syscall.CloseHandle(waitHandle))"},
	}
	for _, r := range replacements {
		s = strings.ReplaceAll(s, r[0], r[1])
	}
	if a := strings.Index(s, "all := []*os.File"); a >= 0 {
		b := strings.Index(s[a:], "handles :=") + a
		s = s[:a] + `success:=false
   defer func(){
    for _,f:=range []*os.File{inR,outW,errW}{j.resources.record("launch_pipe",f.Close())}
    if !success {for _,f:=range []*os.File{inW,outR,errR}{j.resources.record("launch_pipe",f.Close())}}
   }()
  ` + s[b:]
	}
	for _, name := range []string{"inR", "inW", "outR", "outW"} {
		s = strings.ReplaceAll(s, name+".Close()", "j.resources.record(\"launch_pipe\","+name+".Close())")
	}
	return s
}
