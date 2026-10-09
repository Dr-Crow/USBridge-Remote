//go:build !linux || android || wayland

package gui

func (mw *MainWindow) pinLinuxDPIScale() {}
