//go:build windows

// SPDX-License-Identifier: GPL-3.0-only
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

func parseGraphicsStaging(raw []byte) (map[string]string, error) {
	var header struct {
		Schema int             `json:"schema_version"`
		DLLs   json.RawMessage `json:"runtime_dlls_sha256"`
	}
	if len(raw) > 65536 || exactJSON(raw, &header, "schema_version", "runtime_dlls_sha256") != nil || header.Schema != 1 {
		return nil, failure("invalid_graphics_staging")
	}
	// Parse the nested object token by token too: ordinary map decoding silently
	// accepts duplicate keys and case aliases that Windows treats as one DLL.
	d := json.NewDecoder(bytes.NewReader(header.DLLs))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, failure("invalid_graphics_staging")
	}
	values, seen := map[string]string{}, map[string]bool{}
	for d.More() {
		token, err = d.Token()
		name, ok := token.(string)
		lower := strings.ToLower(name)
		if err != nil || !ok || !graphicsDLLName.MatchString(name) || seen[lower] || len(values) >= 256 {
			return nil, failure("invalid_graphics_staging")
		}
		var digest string
		if d.Decode(&digest) != nil || !hashPattern.MatchString(digest) {
			return nil, failure("invalid_graphics_staging")
		}
		seen[lower], values[name] = true, digest
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') {
		return nil, failure("invalid_graphics_staging")
	}
	if _, err = d.Token(); err != io.EOF || !seen["opengl32.dll"] || !seen["libgallium_wgl.dll"] {
		return nil, failure("invalid_graphics_staging")
	}
	return values, nil
}

// MODULEENTRY32W, Windows amd64. Snapshot only the one owned process, never
// other processes or desktops. Full module paths remain in memory only.
type graphicsModuleEntry struct {
	Size, ModuleID, ProcessID, GlobalUsage, ProcessUsage uint32
	BaseAddress                                          uintptr
	BaseSize                                             uint32
	Module                                               syscall.Handle
	Name                                                 [256]uint16
	Path                                                 [260]uint16
}

func graphicsPath(path string) string {
	return filepath.Clean(strings.TrimPrefix(path, `\\?\`))
}

func graphicsModules(pid uint32, executable, systemRoot string, expected map[string]string) (map[string]string, error) {
	if pid == 0 || !drivePath.MatchString(executable) || !drivePath.MatchString(systemRoot) {
		return nil, failure("graphics_owned_process_identity_invalid")
	}
	if unsafe.Sizeof(graphicsModuleEntry{}) != 1080 || unsafe.Offsetof(graphicsModuleEntry{}.Path) != 560 {
		return nil, failure("graphics_module_layout_mismatch")
	}
	create := kernel32.NewProc("CreateToolhelp32Snapshot")
	var snapshot uintptr
	var err error
	// Windows documents ERROR_BAD_LENGTH as a transient module-list race.
	deadline := time.Now().Add(time.Second)
	for {
		snapshot, _, err = create.Call(0x8|0x10, uintptr(pid))
		if snapshot != ^uintptr(0) {
			break
		}
		if err != syscall.Errno(24) || time.Now().After(deadline) { // ERROR_BAD_LENGTH
			return nil, failure("graphics_module_snapshot_failed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer syscall.CloseHandle(syscall.Handle(snapshot))
	entry := graphicsModuleEntry{Size: uint32(unsafe.Sizeof(graphicsModuleEntry{}))}
	first, next := kernel32.NewProc("Module32FirstW"), kernel32.NewProc("Module32NextW")
	ok, _, _ := first.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	if ok == 0 {
		return nil, failure("graphics_module_inventory_failed")
	}
	allowed := map[string]string{}
	for name := range expected {
		allowed[strings.ToLower(name)] = name
	}
	seen, loaded := map[string]bool{}, map[string]string{}
	directory := filepath.Dir(executable)
	system := strings.ToLower(graphicsPath(filepath.Join(systemRoot, "System32"))) + string(filepath.Separator)
	for count := 0; ; count++ {
		if count >= 512 || entry.ProcessID != pid || entry.Name[len(entry.Name)-1] != 0 || entry.Path[len(entry.Path)-1] != 0 {
			return nil, failure("graphics_module_inventory_invalid")
		}
		name, path := syscall.UTF16ToString(entry.Name[:]), graphicsPath(syscall.UTF16ToString(entry.Path[:]))
		lower := strings.ToLower(name)
		if name == "" || seen[lower] || !strings.EqualFold(filepath.Base(path), name) {
			return nil, failure("graphics_module_inventory_invalid")
		}
		seen[lower] = true
		if declared, present := allowed[lower]; present {
			if !strings.EqualFold(path, graphicsPath(filepath.Join(directory, declared))) {
				return nil, rejectedGraphicsModule("graphics_module_not_app_local", name, path, directory, systemRoot, true)
			}
			loaded[declared] = expected[declared]
		} else if !strings.EqualFold(path, graphicsPath(executable)) && !strings.HasPrefix(strings.ToLower(path), system) {
			return nil, rejectedGraphicsModule("graphics_module_not_in_verified_closure", name, path, directory, systemRoot, false)
		}
		entry = graphicsModuleEntry{Size: uint32(unsafe.Sizeof(graphicsModuleEntry{}))}
		ok, _, err = next.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
		if ok == 0 {
			if err != syscall.ERROR_NO_MORE_FILES {
				return nil, failure("graphics_module_inventory_failed")
			}
			break
		}
	}
	for _, name := range []string{"opengl32.dll", "libgallium_wgl.dll"} {
		if declared := allowed[name]; declared == "" || loaded[declared] == "" {
			return nil, failure("graphics_mesa_module_missing")
		}
	}
	return loaded, nil
}

// Keep the supplied pins alive until the owned process has naturally retired.
func pinGraphicsStaging(pins *pins, executable, staging, stagingSHA string) (map[string]string, error) {
	f, err := pins.check(staging, stagingSHA, 65536)
	if err != nil {
		return nil, err
	}
	raw, err := readPinned(f, 65536)
	if err != nil {
		return nil, err
	}
	expected, err := parseGraphicsStaging(raw)
	clear(raw)
	if err != nil {
		return nil, err
	}
	for name, digest := range expected {
		path := filepath.Join(filepath.Dir(executable), name)
		if _, err := pins.check(path, digest, 512<<20); err != nil {
			return nil, err
		}
		path16, err := syscall.UTF16PtrFromString(path)
		if err != nil {
			return nil, failure("graphics_dll_path_invalid")
		}
		attributes, err := syscall.GetFileAttributes(path16)
		if err != nil || attributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return nil, failure("graphics_dll_reparse_point")
		}
	}
	return expected, nil
}
