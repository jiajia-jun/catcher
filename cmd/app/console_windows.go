//go:build windows

package app

import (
	"os"

	"golang.org/x/sys/windows"
)

// attachParentProcess 对应 ATTACH_PARENT_PROCESS
const attachParentProcess = ^uint32(0)

var procAttachConsole = windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")

// attachParentConsole 在标准输出或标准错误不可用时接入父进程的控制台
//
// GUI 子系统程序从控制台启动时拿不到可用的标准句柄，直接打印的内容会被丢弃；
// 管道与文件重定向的场景句柄有效，保持原样
func attachParentConsole() {
	outOK := stdHandleUsable(windows.STD_OUTPUT_HANDLE)
	errOK := stdHandleUsable(windows.STD_ERROR_HANDLE)
	if outOK && errOK {
		return
	}
	if r, _, _ := procAttachConsole.Call(uintptr(attachParentProcess)); r == 0 {
		return
	}
	if !outOK {
		if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
			os.Stdout = f
		}
	}
	if !errOK {
		if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
			os.Stderr = f
		}
	}
}

// stdHandleUsable 判断指定标准句柄是否指向可写目标
func stdHandleUsable(kind uint32) bool {
	handle, err := windows.GetStdHandle(kind)
	if err != nil || handle == 0 || handle == windows.InvalidHandle {
		return false
	}
	fileType, err := windows.GetFileType(handle)
	if err != nil {
		return false
	}
	return fileType != windows.FILE_TYPE_UNKNOWN
}
