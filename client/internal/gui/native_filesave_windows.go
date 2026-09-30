//go:build windows

package gui

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

func nativeSaveAvailable() bool { return true }

func nativeSaveFile(title, defaultName string) (string, error) {
	script := fmt.Sprintf(`
Add-Type -AssemblyName System.Windows.Forms
$dialog = New-Object System.Windows.Forms.SaveFileDialog
$dialog.Title = %q
$dialog.Filter = "ZIP archive|*.zip|All files|*.*"
$dialog.FileName = %q
$dialog.AddExtension = $true
$dialog.DefaultExt = "zip"
$dialog.OverwritePrompt = $true
$dialog.CheckPathExists = $true
if ($dialog.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) {
	[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
	Write-Output $dialog.FileName
}
`, title, defaultName)

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-STA", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
