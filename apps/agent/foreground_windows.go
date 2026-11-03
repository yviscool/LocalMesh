//go:build windows

package main

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modUser32FG = windows.NewLazySystemDLL("user32.dll")
	modKernelFG = windows.NewLazySystemDLL("kernel32.dll")

	procGetForegroundWindow   = modUser32FG.NewProc("GetForegroundWindow")
	procGetWindowThreadProcID = modUser32FG.NewProc("GetWindowThreadProcessId")
	procQueryFullProcessImage = modKernelFG.NewProc("QueryFullProcessImageNameW")
)

const processQueryLimitedInformation = 0x1000

// ForegroundProcess returns the executable name of the process owning the
// foreground window, used for program-usage audit samples.
func ForegroundProcess() (string, bool) {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return "", false
	}

	var pid uint32
	procGetWindowThreadProcID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 {
		return "", false
	}

	handle, err := windows.OpenProcess(processQueryLimitedInformation, false, pid)
	if err != nil {
		return "", false
	}
	defer windows.CloseHandle(handle)

	buf := make([]uint16, 512)
	size := uint32(len(buf))
	ret, _, _ := procQueryFullProcessImage.Call(
		uintptr(handle),
		0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if ret == 0 {
		return "", false
	}

	exe := windows.UTF16ToString(buf[:size])
	if idx := strings.LastIndexByte(exe, '\\'); idx >= 0 {
		exe = exe[idx+1:]
	}
	if exe == "" {
		return "", false
	}
	return exe, true
}
