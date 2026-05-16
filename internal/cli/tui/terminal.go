package tui

import (
	"io"
	"os"
	"syscall"
	"unsafe"
)

type terminalSize struct {
	Rows    uint16
	Columns uint16
	XPixels uint16
	YPixels uint16
}

func renderSettings(stdout io.Writer) (int, bool) {
	file, ok := stdout.(*os.File)
	if !ok {
		return defaultRenderWidth, false
	}
	size, ok := queryTerminalSize(file.Fd())
	if !ok {
		return defaultRenderWidth, false
	}
	return int(size.Columns), true
}

func queryTerminalSize(fd uintptr) (terminalSize, bool) {
	var size terminalSize
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&size)))
	if errno != 0 || size.Columns == 0 {
		return terminalSize{}, false
	}
	return size, true
}
