package main

import (
	"image"
	"image/color"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassExW               = user32.NewProc("RegisterClassExW")
	procCreateWindowExW                = user32.NewProc("CreateWindowExW")
	procDefWindowProcW                 = user32.NewProc("DefWindowProcW")
	procGetMessageW                    = user32.NewProc("GetMessageW")
	procTranslateMessage               = user32.NewProc("TranslateMessage")
	procDispatchMessageW               = user32.NewProc("DispatchMessageW")
	procPostMessageW                   = user32.NewProc("PostMessageW")
	procSendMessageW                   = user32.NewProc("SendMessageW")
	procPostQuitMessage                = user32.NewProc("PostQuitMessage")
	procCreatePopupMenu                = user32.NewProc("CreatePopupMenu")
	procAppendMenuW                    = user32.NewProc("AppendMenuW")
	procTrackPopupMenuEx               = user32.NewProc("TrackPopupMenuEx")
	procDestroyMenu                    = user32.NewProc("DestroyMenu")
	procSetForegroundWindow            = user32.NewProc("SetForegroundWindow")
	procGetCursorPos                   = user32.NewProc("GetCursorPos")
	procRegisterWindowMessageW         = user32.NewProc("RegisterWindowMessageW")
	procCreateIconIndirect             = user32.NewProc("CreateIconIndirect")
	procDestroyIcon                    = user32.NewProc("DestroyIcon")
	procRegisterHotKey                 = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey               = user32.NewProc("UnregisterHotKey")
	procGetSystemMetricsForDpi         = user32.NewProc("GetSystemMetricsForDpi")
	procGetDpiForSystem                = user32.NewProc("GetDpiForSystem")
	procSetProcessDpiAwarenessContext  = user32.NewProc("SetProcessDpiAwarenessContext")
	procAllowSetForegroundWindow       = user32.NewProc("AllowSetForegroundWindow")
	procMessageBoxW                    = user32.NewProc("MessageBoxW")
	procShellNotifyIconW               = shell32.NewProc("Shell_NotifyIconW")
	procShellExecuteExW                = shell32.NewProc("ShellExecuteExW")
	procCreateDIBSection               = gdi32.NewProc("CreateDIBSection")
	procCreateBitmap                   = gdi32.NewProc("CreateBitmap")
	procDeleteObject                   = gdi32.NewProc("DeleteObject")
	procGetModuleHandleW               = kernel32.NewProc("GetModuleHandleW")
	procGetUserDefaultUILanguage       = kernel32.NewProc("GetUserDefaultUILanguage")
	procGetNamedPipeServerProcessId    = kernel32.NewProc("GetNamedPipeServerProcessId")
	procSetCurrentProcessExplicitAppID = shell32.NewProc("SetCurrentProcessExplicitAppUserModelID")
)

const (
	wmDestroy       = 0x0002
	wmCommand       = 0x0111
	wmHotKey        = 0x0312
	wmSettingChange = 0x001A
	wmNull          = 0x0000
	wmContextMenu   = 0x007B
	wmApp           = 0x8000
	wmTray          = wmApp + 1 // the notification icon was used
	wmRedraw        = wmApp + 2 // the state changed
	wmSetHotKey     = wmApp + 3 // register the shortcut, on this thread
	wmOpenMenu      = wmApp + 4

	ninSelect    = 0x0400
	ninKeySelect = 0x0401

	nimAdd        = 0
	nimModify     = 1
	nimDelete     = 2
	nimSetVersion = 4
	nifMessage    = 0x01
	nifIcon       = 0x02
	nifTip        = 0x04
	nifShowTip    = 0x80

	mfString    = 0x0000
	mfGrayed    = 0x0001
	mfChecked   = 0x0008
	mfPopup     = 0x0010
	mfSeparator = 0x0800
	mfDefault   = 0x1000 // MFS_DEFAULT: drawn in bold

	tpmReturnCmd   = 0x0100
	tpmRightButton = 0x0002
	tpmBottomAlign = 0x0020

	modNoRepeat = 0x4000
)

type point struct{ X, Y int32 }

type msg struct {
	Hwnd     windows.Handle
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       point
	LPrivate uint32
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   windows.Handle
	Icon       windows.Handle
	Cursor     windows.Handle
	Background windows.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     windows.Handle
}

type notifyIconData struct {
	Size            uint32
	Wnd             windows.Handle
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            windows.Handle
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GuidItem        windows.GUID
	BalloonIcon     windows.Handle
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type iconInfo struct {
	Icon     int32
	XHotspot uint32
	YHotspot uint32
	Mask     windows.Handle
	Color    windows.Handle
}

type shellExecuteInfo struct {
	Size       uint32
	Mask       uint32
	Wnd        windows.Handle
	Verb       *uint16
	File       *uint16
	Parameters *uint16
	Directory  *uint16
	Show       int32
	InstApp    windows.Handle
	IDList     uintptr
	Class      *uint16
	KeyClass   windows.Handle
	HotKey     uint32
	IconOrMon  windows.Handle
	Process    windows.Handle
}

func utf16(s string) *uint16 {
	p, _ := windows.UTF16PtrFromString(s)
	return p
}

// copyTo fills a fixed buffer with s, cut to fit.
func copyTo(buf []uint16, s string) {
	u, _ := windows.UTF16FromString(s)
	if len(u) > len(buf) {
		u = u[:len(buf)]
		u[len(u)-1] = 0
	}
	copy(buf, u)
}

// newIcon turns an image into an icon the system can show.
func newIcon(img *image.NRGBA) windows.Handle {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	header := bitmapInfoHeader{Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 32}
	header.Size = uint32(unsafe.Sizeof(header))
	var bits unsafe.Pointer
	color, _, _ := procCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&header)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if color == 0 {
		return 0
	}
	defer procDeleteObject.Call(color)
	pixels := unsafe.Slice((*byte)(bits), w*h*4)
	for i := 0; i < w*h; i++ {
		pixels[i*4+0] = img.Pix[i*4+2]
		pixels[i*4+1] = img.Pix[i*4+1]
		pixels[i*4+2] = img.Pix[i*4+0]
		pixels[i*4+3] = img.Pix[i*4+3]
	}
	maskBits := make([]byte, (w+15)/16*2*h)
	mask, _, _ := procCreateBitmap.Call(uintptr(w), uintptr(h), 1, 1, uintptr(unsafe.Pointer(&maskBits[0])))
	defer procDeleteObject.Call(mask)
	info := iconInfo{Icon: 1, Mask: windows.Handle(mask), Color: windows.Handle(color)}
	icon, _, _ := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&info)))
	return windows.Handle(icon)
}

// lightTaskbar says whether the taskbar is light, so the icon is drawn dark.
func lightTaskbar() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	v, _, err := key.GetIntegerValue("SystemUsesLightTheme")
	return err == nil && v == 1
}

func inkFor(light bool) color.NRGBA {
	if light {
		return color.NRGBA{0, 0, 0, 255}
	}
	return color.NRGBA{255, 255, 255, 255}
}

// smallIconSize is the size of an icon in the notification area.
func smallIconSize() int {
	const smCxSmIcon = 49
	dpi := uintptr(96)
	if procGetDpiForSystem.Find() == nil {
		dpi, _, _ = procGetDpiForSystem.Call()
	}
	if procGetSystemMetricsForDpi.Find() == nil {
		n, _, _ := procGetSystemMetricsForDpi.Call(smCxSmIcon, dpi)
		if n > 0 {
			return int(n)
		}
	}
	return 16
}

// darkMenus lets the menus follow the dark theme of the system, as the
// system's own icons do. Windows offers it to its programs without
// documenting it: uxtheme's SetPreferredAppMode, ordinal 135, since 1903.
func darkMenus() {
	lib, err := windows.LoadLibrary("uxtheme.dll")
	if err != nil {
		return
	}
	const allowDark = 1
	if set, err := windows.GetProcAddressByOrdinal(lib, 135); err == nil {
		syscall.SyscallN(set, allowDark)
	}
	if flush, err := windows.GetProcAddressByOrdinal(lib, 136); err == nil {
		syscall.SyscallN(flush)
	}
}

func messageBox(title, text string) {
	const mbIconError = 0x10
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(utf16(text))), uintptr(unsafe.Pointer(utf16(title))), mbIconError)
}
