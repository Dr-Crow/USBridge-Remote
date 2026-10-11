//go:build vm330_signature_probe

package main

// VM330-only test process policy, explicitly approved by the operator.
// This does not call Set-ExecutionPolicy, modify any persistent scope, or
// propagate the policy to the viewer/source or the default CI build.
func graphicsSignatureLaunchArguments(script string) []string {
	return []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "RemoteSigned", "-File", script}
}
