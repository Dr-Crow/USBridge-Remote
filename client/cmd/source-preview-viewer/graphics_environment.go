package main

import "strings"

// This dedicated viewer does not accept inherited graphics diagnostics or
// driver overrides. The Windows software fallback is an explicit fixed policy,
// not an arbitrary environment passthrough. Linux retains its existing policy.
func clearGraphicsEnvironmentWith(environment []string, unset func(string) error) error {
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(name)
		for _, prefix := range []string{"MESA_", "GALLIUM_", "LIBGL_", "WGL_", "LP_", "LLVM_", "ZINK_", "D3D12_"} {
			if strings.HasPrefix(upper, prefix) {
				if err := unset(name); err != nil {
					return err
				}
				break
			}
		}
	}
	return nil
}

func setSoftwareGraphicsWith(set func(string, string) error) error {
	if err := set("GALLIUM_DRIVER", "llvmpipe"); err != nil {
		return err
	}
	return set("LIBGL_ALWAYS_SOFTWARE", "true")
}
