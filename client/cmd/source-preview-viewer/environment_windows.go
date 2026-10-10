//go:build windows && cgo

package main

/*
#include <stdlib.h>
#include <string.h>
#include <wchar.h>

static int preview_wide_environment_probe(int set) {
    if (set) return _wputenv_s(L"USBRIDGE_WIDE_ONLY", L"native-wide-private-diagnostic");
    return _wgetenv(L"USBRIDGE_WIDE_ONLY") != NULL;
}

// MinGW-w64's UCRT stdlib.h exposes these accessors (also used by its
// _environ/_wenviron macros), not Microsoft's _get_environ/_get_wenviron.
// Reacquire the current array after mutations; a NULL array is valid when
// that character-width environment has not been initialized yet.
static int preview_get_environ(char ***out) {
    char ***current = __p__environ();
    if (!current) return -1;
    *out = *current;
    return 0;
}
static int preview_get_wenviron(wchar_t ***out) {
    wchar_t ***current = __p__wenviron();
    if (!current) return -1;
    *out = *current;
    return 0;
}

// UCRT environment copies can differ from Win32's environment block. Enumerate
// both copies directly, restarting after each erase because _putenv_s can move
// the array. Only variable names are copied; values never leave native memory.
static int preview_clear_crt_environment(void) {
    char **env = NULL;
    if (preview_get_environ(&env) != 0) return -1;
    for (size_t i = 0; env && env[i];) {
        if (_strnicmp(env[i], "USBRIDGE_", 9) != 0) { ++i; continue; }
        char *eq = strchr(env[i], '=');
        if (!eq) return -1;
        size_t n = (size_t)(eq - env[i]);
        char *name = (char *)malloc(n + 1);
        if (!name) return -1;
        memcpy(name, env[i], n); name[n] = 0;
        int result = _putenv_s(name, "");
        free(name);
        if (result != 0 || preview_get_environ(&env) != 0) return -1;
        i = 0;
    }
    wchar_t **wide = NULL;
    if (preview_get_wenviron(&wide) != 0) return -1;
    for (size_t i = 0; wide && wide[i];) {
        if (_wcsnicmp(wide[i], L"USBRIDGE_", 9) != 0) { ++i; continue; }
        wchar_t *eq = wcschr(wide[i], L'=');
        if (!eq) return -1;
        size_t n = (size_t)(eq - wide[i]);
        wchar_t *name = (wchar_t *)malloc((n + 1) * sizeof(wchar_t));
        if (!name) return -1;
        memcpy(name, wide[i], n * sizeof(wchar_t)); name[n] = 0;
        int result = _wputenv_s(name, L"");
        free(name);
        if (result != 0 || preview_get_wenviron(&wide) != 0) return -1;
        i = 0;
    }
    return 0;
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

func clearPreviewNativeEnvironment() error {
	if C.preview_clear_crt_environment() != 0 {
		return errors.New("could not clear inherited preview configuration")
	}
	return nil
}

// The native test checks C getenv itself, not just Go's Win32-backed cache.
func previewCRTLookupEnv(name string) (string, bool) {
	key := C.CString(name)
	defer C.free(unsafe.Pointer(key))
	value := C.getenv(key)
	if value == nil {
		return "", false
	}
	return C.GoString(value), true
}

func previewCRTSetenv(name, value string) error {
	key, val := C.CString(name), C.CString(value)
	defer C.free(unsafe.Pointer(key))
	defer C.free(unsafe.Pointer(val))
	if C._putenv_s(key, val) != 0 {
		return errors.New("could not set native test environment")
	}
	return nil
}

func previewCRTSetWideProbe() error {
	if C.preview_wide_environment_probe(1) != 0 {
		return errors.New("could not set native wide test environment")
	}
	return nil
}
func previewCRTContainsWideProbe() bool { return C.preview_wide_environment_probe(0) != 0 }
