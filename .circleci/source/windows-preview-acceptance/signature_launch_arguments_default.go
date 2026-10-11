//go:build !vm331_signature_probe

package main

func graphicsSignatureLaunchArguments(script string) []string {
	return graphicsSignatureArguments(script)
}
