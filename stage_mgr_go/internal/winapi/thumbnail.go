package winapi

import "unsafe"

var (
	procDwmRegisterThumbnail         = dwmapi.NewProc("DwmRegisterThumbnail")
	procDwmUpdateThumbnailProperties = dwmapi.NewProc("DwmUpdateThumbnailProperties")
	procDwmUnregisterThumbnail       = dwmapi.NewProc("DwmUnregisterThumbnail")
)

// Thumbnail shows a window's picture inside another, as DWM composes it:
// live, with its transparency, even while the window is off the desktop.
type Thumbnail uintptr

type thumbnailProperties struct {
	Flags                   uint32
	Destination, Source     Rect
	Opacity                 byte
	Visible, OnlyClientArea int32
}

const (
	tnpRectDestination = 0x1
	tnpRectSource      = 0x2
	tnpOpacity         = 0x4
	tnpVisible         = 0x8
	tnpOnlyClientArea  = 0x10
)

// RegisterThumbnail shows src inside dest, hidden until Show places it.
func RegisterThumbnail(dest, src HWND) (Thumbnail, bool) {
	var t Thumbnail
	hr, _, _ := procDwmRegisterThumbnail.Call(uintptr(dest), uintptr(src), uintptr(unsafe.Pointer(&t)))
	if hr != 0 {
		return 0, false
	}
	t.update(thumbnailProperties{Flags: tnpVisible})
	return t, true
}

// Show draws the part source of the window, in its window coordinates, at
// dest in the client area of the window showing it. DWM takes it in with
// the next frame it composes.
func (t Thumbnail) Show(dest, source Rect, opacity float64) {
	if opacity <= 0 || dest.Empty() || source.Empty() {
		t.Hide()
		return
	}
	t.update(thumbnailProperties{
		Flags:       tnpRectDestination | tnpRectSource | tnpOpacity | tnpVisible | tnpOnlyClientArea,
		Destination: dest, Source: source,
		Opacity: byte(min(opacity, 1)*255 + 0.5), Visible: 1,
	})
}

func (t Thumbnail) Hide() { t.update(thumbnailProperties{Flags: tnpVisible}) }

func (t Thumbnail) Unregister() { procDwmUnregisterThumbnail.Call(uintptr(t)) }

func (t Thumbnail) update(p thumbnailProperties) {
	procDwmUpdateThumbnailProperties.Call(uintptr(t), uintptr(unsafe.Pointer(&p)))
}
