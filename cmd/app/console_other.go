//go:build !windows

package app

// attachParentConsole 在非 Windows 平台没有对应机制
func attachParentConsole() {}
