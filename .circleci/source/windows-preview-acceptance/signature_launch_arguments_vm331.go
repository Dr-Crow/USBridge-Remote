//go:build vm331_signature_probe

package main

// VM331-only test process policy, requires separate VM331 operator approval before execution.
// This does not call Set-ExecutionPolicy, modify any persistent scope, or
// propagate the policy to the viewer/source or the default CI build.
func graphicsSignatureLaunchArguments(script string) []string {
	return []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "RemoteSigned", "-File", script}
}
