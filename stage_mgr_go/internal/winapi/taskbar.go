//go:build windows

package winapi

import (
	"errors"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

var (
	ole32    = syscall.NewLazyDLL("ole32.dll")
	oleaut32 = syscall.NewLazyDLL("oleaut32.dll")

	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoUninitialize   = ole32.NewProc("CoUninitialize")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
	procSysFreeString    = oleaut32.NewProc("SysFreeString")
)

// TaskbarButton is the button of an app on a taskbar.
type TaskbarButton struct {
	// Name is what the taskbar reads out: the app's name, then how many
	// windows it has ("微信 - 1 个运行窗口 已固定").
	Name string
	// AppID is the AppUserModelID the taskbar groups the app's windows by:
	// the path of its program, often relative to a known folder, or an ID
	// the app chose.
	AppID string
	Rect  Rect
}

// TaskbarButtons lists the app buttons of a taskbar window (Shell_TrayWnd,
// or Shell_SecondaryTrayWnd on another display). It asks the shell through
// UI Automation, which takes some tens of milliseconds: call it off the
// main thread.
func TaskbarButtons(tray HWND) ([]TaskbarButton, error) {
	// COM wants the calls of one apartment on one thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	const coinitMultithreaded = 0x0
	if hr, _, _ := procCoInitializeEx.Call(0, coinitMultithreaded); int32(hr) < 0 {
		return nil, errors.New("CoInitializeEx failed")
	}
	defer procCoUninitialize.Call()

	clsidCUIAutomation := syscall.GUID{Data1: 0xff48dba4, Data2: 0x60ef, Data3: 0x4201, Data4: [8]byte{0xaa, 0x87, 0x54, 0x10, 0x3e, 0xef, 0x59, 0x4e}}
	iidIUIAutomation := syscall.GUID{Data1: 0x30cbe57d, Data2: 0xd9d0, Data3: 0x452a, Data4: [8]byte{0xab, 0x13, 0x7a, 0xc5, 0xac, 0x48, 0x25, 0xee}}
	const clsctxInprocServer = 0x1
	var uia *comObject
	if hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidCUIAutomation)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIUIAutomation)), uintptr(unsafe.Pointer(&uia))); int32(hr) < 0 || uia == nil {
		return nil, errors.New("UI Automation unavailable")
	}
	defer uia.release()

	const (
		uiaElementFromHandle       = 6
		uiaCreatePropertyCondition = 23
		elemFindAll                = 6
		elemCurrentName            = 23
		elemCurrentAutomationID    = 29
		elemCurrentClassName       = 30
		elemCurrentBoundingRect    = 43
		arrayLength                = 3
		arrayGetElement            = 4

		treeScopeDescendants    = 4
		uiaControlTypePropertyID = 30003
		uiaButtonControlTypeID  = 50000
		vtI4                    = 3
	)
	var root *comObject
	if uia.call(uiaElementFromHandle, uintptr(tray), uintptr(unsafe.Pointer(&root))) < 0 || root == nil {
		return nil, errors.New("no UI Automation element for the taskbar")
	}
	defer root.release()
	// A VARIANT holding the button control type, passed by reference as
	// structs over 8 bytes are.
	variant := struct {
		vt       uint16
		reserved [3]uint16
		val      int64
		pad      int64
	}{vt: vtI4, val: uiaButtonControlTypeID}
	var cond *comObject
	if uia.call(uiaCreatePropertyCondition, uiaControlTypePropertyID, uintptr(unsafe.Pointer(&variant)), uintptr(unsafe.Pointer(&cond))) < 0 || cond == nil {
		return nil, errors.New("CreatePropertyCondition failed")
	}
	defer cond.release()
	var all *comObject
	if root.call(elemFindAll, treeScopeDescendants, uintptr(unsafe.Pointer(cond)), uintptr(unsafe.Pointer(&all))) < 0 || all == nil {
		return nil, errors.New("FindAll failed")
	}
	defer all.release()
	var n int32
	all.call(arrayLength, uintptr(unsafe.Pointer(&n)))
	var out []TaskbarButton
	for i := range n {
		var el *comObject
		if all.call(arrayGetElement, uintptr(i), uintptr(unsafe.Pointer(&el))) < 0 || el == nil {
			continue
		}
		id := el.bstr(elemCurrentAutomationID)
		// The taskbar names its app buttons' IDs "Appid: <AppUserModelID>";
		// the start button and the tray icons are buttons too.
		if appID, ok := strings.CutPrefix(id, "Appid: "); ok {
			b := TaskbarButton{Name: el.bstr(elemCurrentName), AppID: appID}
			el.call(elemCurrentBoundingRect, uintptr(unsafe.Pointer(&b.Rect)))
			out = append(out, b)
		}
		el.release()
	}
	return out, nil
}

// comObject is a COM interface pointer: a pointer to its method table.
type comObject struct{ vtbl *[64]uintptr }

func (o *comObject) call(method int, args ...uintptr) int32 {
	hr, _, _ := syscall.SyscallN(o.vtbl[method], append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	return int32(hr)
}

func (o *comObject) release() { syscall.SyscallN(o.vtbl[2], uintptr(unsafe.Pointer(o))) }

// bstr calls a property getter that returns a BSTR, and frees it.
func (o *comObject) bstr(method int) string {
	var p *uint16
	if o.call(method, uintptr(unsafe.Pointer(&p))) < 0 || p == nil {
		return ""
	}
	defer procSysFreeString.Call(uintptr(unsafe.Pointer(p)))
	// A BSTR keeps its length in bytes just before its characters.
	n := *(*uint32)(unsafe.Add(unsafe.Pointer(p), -4)) / 2
	return syscall.UTF16ToString(unsafe.Slice(p, n))
}
