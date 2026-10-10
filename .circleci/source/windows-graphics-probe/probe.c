/* SPDX-License-Identifier: GPL-3.0-only
 * CI-only WGL capability probe. This is not the source-preview viewer.
 * Owns one hidden window/context; no capture, input, network or subprocesses.
 */
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <stdio.h>
#include <string.h>
#include <wchar.h>

typedef int (WINAPI *choose_format_fn)(HDC, const PIXELFORMATDESCRIPTOR *);
typedef BOOL (WINAPI *set_format_fn)(HDC, int, const PIXELFORMATDESCRIPTOR *);
typedef HGLRC (WINAPI *create_context_fn)(HDC);
typedef BOOL (WINAPI *delete_context_fn)(HGLRC);
typedef BOOL (WINAPI *make_current_fn)(HDC, HGLRC);
typedef HGLRC (WINAPI *current_context_fn)(void);
typedef HDC (WINAPI *current_dc_fn)(void);
typedef const unsigned char *(WINAPI *get_string_fn)(unsigned int);

static int local_dll_path(wchar_t *path, size_t capacity, const wchar_t *name) {
    DWORD n = GetModuleFileNameW(NULL, path, (DWORD)capacity);
    if (!n || n >= capacity) return 0;
    wchar_t *slash = wcsrchr(path, L'\\');
    if (!slash || (size_t)(slash + 1 - path) + wcslen(name) >= capacity) return 0;
    wcscpy(slash + 1, name);
    DWORD attributes = GetFileAttributesW(path);
    return attributes != INVALID_FILE_ATTRIBUTES &&
        !(attributes & (FILE_ATTRIBUTE_DIRECTORY | FILE_ATTRIBUTE_REPARSE_POINT));
}

/* Preserve only a private output handle before silencing native diagnostics. */
static HANDLE private_output(void) {
    HANDLE original = GetStdHandle(STD_OUTPUT_HANDLE), output = NULL;
    HANDLE input = GetStdHandle(STD_INPUT_HANDLE);
    if (GetFileType(original) != FILE_TYPE_PIPE || GetFileType(input) != FILE_TYPE_PIPE)
        return NULL;
    if (!DuplicateHandle(GetCurrentProcess(), original, GetCurrentProcess(),
                         &output, 0, FALSE, DUPLICATE_SAME_ACCESS)) return NULL;
    if (!SetHandleInformation(original, HANDLE_FLAG_INHERIT, 0) ||
        !SetHandleInformation(input, HANDLE_FLAG_INHERIT, 0) ||
        !freopen("NUL", "wb", stdout) || !freopen("NUL", "wb", stderr)) {
        CloseHandle(output);
        return NULL;
    }
    HANDLE null_output = CreateFileW(L"NUL", GENERIC_WRITE, FILE_SHARE_READ | FILE_SHARE_WRITE,
                                    NULL, OPEN_EXISTING, 0, NULL);
    if (null_output == INVALID_HANDLE_VALUE ||
        !SetStdHandle(STD_OUTPUT_HANDLE, null_output) ||
        !SetStdHandle(STD_ERROR_HANDLE, null_output)) {
        if (null_output != INVALID_HANDLE_VALUE) CloseHandle(null_output);
        CloseHandle(output);
        return NULL;
    }
    return output;
}

static int copy_gl_string(char value[256], const unsigned char *source) {
    if (!source) return 0;
    for (size_t i = 0; i < 256; ++i) {
        unsigned char c = source[i];
        if (!c) { value[i] = 0; return i != 0; }
        if (c < 32 || c > 126) return 0;
        value[i] = (char)c;
    }
    return 0;
}

static char *json_string(char *at, const char *value) {
    *at++ = '"';
    while (*value) {
        if (*value == '"' || *value == '\\') *at++ = '\\';
        *at++ = *value++;
    }
    *at++ = '"';
    return at;
}

int main(int argc, char **argv) {
    (void)argv;
    if (argc != 1) return 2;
    HANDLE output = private_output();
    if (!output) return 2;
    int result = 2;
    HMODULE gl = NULL;
    HWND window = NULL;
    HDC dc = NULL;
    HGLRC context = NULL;
    make_current_fn make_current = NULL;
    delete_context_fn delete_context = NULL;
    ATOM window_class = 0;
    HINSTANCE instance = GetModuleHandleW(NULL);
    const wchar_t *class_name = L"OwnedSoftwareGraphicsProbe";
    wchar_t path[32768], gallium[32768];
    if (!SetEnvironmentVariableW(L"GALLIUM_DRIVER", L"llvmpipe") ||
        !SetEnvironmentVariableW(L"LIBGL_ALWAYS_SOFTWARE", L"true") ||
        !local_dll_path(path, 32768, L"opengl32.dll") ||
        !local_dll_path(gallium, 32768, L"libgallium_wgl.dll") ||
        GetModuleHandleW(L"opengl32.dll") != NULL) goto cleanup;
    /* Only the executable's verified directory and System32 may resolve imports. */
    gl = LoadLibraryExW(path, NULL, LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR | LOAD_LIBRARY_SEARCH_SYSTEM32);
    if (!gl) goto cleanup;
    choose_format_fn choose_format;
    set_format_fn set_format;
    create_context_fn create_context;
    current_context_fn current_context;
    current_dc_fn current_dc;
    get_string_fn get_string;
    /* memcpy avoids incompatible function-pointer casts under -Wextra. */
#define RESOLVE(variable, name) do { \
    FARPROC address = GetProcAddress(gl, name); \
    _Static_assert(sizeof(variable) == sizeof(address), "function pointer ABI"); \
    memcpy(&(variable), &address, sizeof(variable)); \
    if (!(variable)) goto cleanup; \
} while (0)
    RESOLVE(choose_format, "wglChoosePixelFormat");
    RESOLVE(set_format, "wglSetPixelFormat");
    RESOLVE(create_context, "wglCreateContext");
    RESOLVE(delete_context, "wglDeleteContext");
    RESOLVE(make_current, "wglMakeCurrent");
    RESOLVE(current_context, "wglGetCurrentContext");
    RESOLVE(current_dc, "wglGetCurrentDC");
    RESOLVE(get_string, "glGetString");
#undef RESOLVE
    WNDCLASSW wc = {0};
    wc.style = CS_OWNDC;
    wc.lpfnWndProc = DefWindowProcW;
    wc.hInstance = instance;
    wc.lpszClassName = class_name;
    window_class = RegisterClassW(&wc);
    if (!window_class) goto cleanup;
    window = CreateWindowExW(0, class_name, L"Owned graphics capability probe",
                             WS_OVERLAPPEDWINDOW, 0, 0, 64, 64, NULL, NULL, instance, NULL);
    if (!window || IsWindowVisible(window)) goto cleanup;
    DWORD owner = 0;
    if (!GetWindowThreadProcessId(window, &owner) || owner != GetCurrentProcessId()) goto cleanup;
    dc = GetDC(window); /* This owned DC is used only to create a context, never read pixels. */
    if (!dc) goto cleanup;
    PIXELFORMATDESCRIPTOR pfd = {0};
    pfd.nSize = sizeof(pfd);
    pfd.nVersion = 1;
    pfd.dwFlags = PFD_DRAW_TO_WINDOW | PFD_SUPPORT_OPENGL | PFD_DOUBLEBUFFER;
    pfd.iPixelType = PFD_TYPE_RGBA;
    pfd.cColorBits = 24;
    pfd.cDepthBits = 16;
    int format = choose_format(dc, &pfd);
    if (!format || !set_format(dc, format, &pfd)) goto cleanup;
    context = create_context(dc);
    if (!context || !make_current(dc, context) ||
        current_context() != context || current_dc() != dc) goto cleanup;
    char vendor[256], renderer[256], version[256];
    if (!copy_gl_string(vendor, get_string(0x1F00)) ||
        !copy_gl_string(renderer, get_string(0x1F01)) ||
        !copy_gl_string(version, get_string(0x1F02))) goto cleanup;
    if (strncmp(renderer, "llvmpipe (", 10) != 0) goto cleanup;
    char json[2048];
    const char *prefix = "{\"schema_version\":1,\"role\":\"owned-wgl-probe-only\",\"gl_vendor\":";
    strcpy(json, prefix);
    char *at = json_string(json + strlen(json), vendor);
    const char *middle = ",\"gl_renderer\":";
    memcpy(at, middle, strlen(middle)); at += strlen(middle);
    at = json_string(at, renderer);
    middle = ",\"gl_version\":";
    memcpy(at, middle, strlen(middle)); at += strlen(middle);
    at = json_string(at, version);
    *at++ = '}'; *at++ = '\n';
    DWORD written = 0, length = (DWORD)(at - json);
    if (!WriteFile(output, json, length, &written, NULL) || written != length) goto cleanup;
    /* Keep modules/context alive for the parent's PID-scoped provenance check. */
    unsigned char extra;
    DWORD read = 0;
    BOOL ok = ReadFile(GetStdHandle(STD_INPUT_HANDLE), &extra, 1, &read, NULL);
    if ((ok && read == 0) || (!ok && GetLastError() == ERROR_BROKEN_PIPE)) result = 0;
cleanup:
    if (context) {
        if (!make_current(NULL, NULL) || !delete_context(context)) result = 2;
    }
    if (dc && !ReleaseDC(window, dc)) result = 2;
    if (window && !DestroyWindow(window)) result = 2;
    if (window_class && !UnregisterClassW(class_name, instance)) result = 2;
    if (gl && !FreeLibrary(gl)) result = 2;
    if (!CloseHandle(output)) result = 2;
    return result;
}
