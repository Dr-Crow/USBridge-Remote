//go:build !vm330_signature_probe

package main

func graphicsSignatureLaunchArguments(script string) []string {
	return graphicsSignatureArguments(script)
}
