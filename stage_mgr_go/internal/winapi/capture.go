//go:build windows

package winapi

import (
	"image"
	"runtime"
	"syscall"
	"unsafe"
)

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

// dib is a top-down 32-bit bitmap selected into a memory DC.
type dib struct {
	dc, bmp, old uintptr
	bits         unsafe.Pointer
	w, h         int
}

func newDIB(w, h int) *dib {
	dc, _, _ := procCreateCompatibleDC.Call(0)
	if dc == 0 {
		return nil
	}
	bi := bitmapInfoHeader{Size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 32}
	var bits unsafe.Pointer
	bmp, _, _ := procCreateDIBSection.Call(dc, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 || bits == nil {
		procDeleteDC.Call(dc)
		return nil
	}
	old, _, _ := procSelectObject.Call(dc, bmp)
	return &dib{dc: dc, bmp: bmp, old: old, bits: bits, w: w, h: h}
}

func (d *dib) pixels() []byte { return unsafe.Slice((*byte)(d.bits), d.w*d.h*4) }

func (d *dib) free() {
	procSelectObject.Call(d.dc, d.old)
	procDeleteObject.Call(d.bmp)
	procDeleteDC.Call(d.dc)
}

// Capture takes a still of a window's visible frame, at most maxHeight
// pixels tall, by asking it to draw itself (PrintWindow with full content,
// which includes what it draws with DirectX); nil for a minimized window or
// one that drew nothing. The window's own thread draws the picture, so a
// hung window can hold the call up: call it off the main thread.
func Capture(h HWND, maxHeight int) *image.RGBA {
	if !IsWindow(h) || IsIconic(h) || maxHeight <= 0 {
		return nil
	}
	bounds := WindowRect(h)
	frame := FrameBounds(h)
	if bounds.Empty() {
		return nil
	}
	// The visible frame inside the window rectangle: DWM draws no resize borders.
	vis := image.Rect(int(frame.Left-bounds.Left), int(frame.Top-bounds.Top), int(frame.Right-bounds.Left), int(frame.Bottom-bounds.Top)).
		Intersect(image.Rect(0, 0, int(bounds.W()), int(bounds.H())))
	if vis.Empty() {
		return nil
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	d := newDIB(int(bounds.W()), int(bounds.H()))
	if d == nil {
		return nil
	}
	defer d.free()
	const pwRenderFullContent = 2
	if r, _, _ := procPrintWindow.Call(uintptr(h), d.dc, pwRenderFullContent); r == 0 {
		return nil
	}
	// Shrink with GDI's halftone filter, which averages what it drops,
	// before anything is copied out.
	out := vis
	dst := d
	if vis.Dy() > maxHeight {
		w := max(1, vis.Dx()*maxHeight/vis.Dy())
		small := newDIB(w, maxHeight)
		if small == nil {
			return nil
		}
		defer small.free()
		const halftone = 4
		procSetStretchBltMode.Call(small.dc, halftone)
		procSetBrushOrgEx.Call(small.dc, 0, 0, 0)
		const srcCopy = 0x00CC0020
		procStretchBlt.Call(small.dc, 0, 0, uintptr(w), uintptr(maxHeight),
			d.dc, uintptr(vis.Min.X), uintptr(vis.Min.Y), uintptr(vis.Dx()), uintptr(vis.Dy()), srcCopy)
		dst, out = small, image.Rect(0, 0, w, maxHeight)
	}
	src := dst.pixels()
	blank := true
	img := image.NewRGBA(image.Rect(0, 0, out.Dx(), out.Dy()))
	for y := 0; y < out.Dy(); y++ {
		row := src[((out.Min.Y+y)*dst.w+out.Min.X)*4:]
		px := img.Pix[y*img.Stride:]
		for x := 0; x < out.Dx(); x++ {
			b, g, r := row[x*4], row[x*4+1], row[x*4+2]
			if blank && (r|g|b) != 0 {
				blank = false
			}
			px[x*4], px[x*4+1], px[x*4+2], px[x*4+3] = r, g, b, 255
		}
	}
	if blank {
		return nil // how PrintWindow fails for windows it cannot draw
	}
	return img
}

type iconInfo struct {
	FIcon    int32
	XHotspot uint32
	YHotspot uint32
	HbmMask  uintptr
	HbmColor uintptr
}

type bitmap struct {
	Type       int32
	Width      int32
	Height     int32
	WidthBytes int32
	Planes     uint16
	BitsPixel  uint16
	Bits       uintptr
}

// WindowIcon returns the large icon a window shows in Alt+Tab, or nil.
func WindowIcon(h HWND) *image.NRGBA {
	const wmGetIcon, iconBig, iconSmall2, smtoAbortIfHung = 0x007F, 1, 2, 0x0002
	const gclpHIcon = -14
	var hicon uintptr
	for _, kind := range []uintptr{iconBig, iconSmall2} {
		procSendMessageTimeoutW.Call(uintptr(h), wmGetIcon, kind, 0, smtoAbortIfHung, 100, uintptr(unsafe.Pointer(&hicon)))
		if hicon != 0 {
			break
		}
	}
	if hicon == 0 {
		hicon, _, _ = procGetClassLongPtrW.Call(uintptr(h), int32ToUintptr(gclpHIcon))
	}
	if hicon == 0 {
		return nil
	}
	return iconImage(hicon)
}

// FileIcon returns the first icon a program holds, drawn from its largest
// image at size by size pixels, or nil. The shell instead puts an icon that
// has no image that large, small and in a faint frame, in the middle of a
// tile of that size.
func FileIcon(path string, size int) *image.NRGBA {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil
	}
	var hicon, id uintptr
	n, _, _ := procPrivateExtractIconsW.Call(uintptr(unsafe.Pointer(p)), 0, uintptr(size), uintptr(size),
		uintptr(unsafe.Pointer(&hicon)), uintptr(unsafe.Pointer(&id)), 1, 0)
	if uint32(n) != 1 || hicon == 0 {
		return nil
	}
	defer procDestroyIcon.Call(hicon)
	return iconImage(hicon)
}

// iconImage converts an HICON, which stays the caller's, to an image.
func iconImage(hicon uintptr) *image.NRGBA {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var ii iconInfo
	if r, _, _ := procGetIconInfo.Call(hicon, uintptr(unsafe.Pointer(&ii))); r == 0 {
		return nil
	}
	defer procDeleteObject.Call(ii.HbmMask)
	if ii.HbmColor == 0 {
		return nil
	}
	defer procDeleteObject.Call(ii.HbmColor)
	var bm bitmap
	procGetObjectW.Call(ii.HbmColor, unsafe.Sizeof(bm), uintptr(unsafe.Pointer(&bm)))
	w, h := int(bm.Width), int(bm.Height)
	if w <= 0 || h <= 0 {
		return nil
	}
	dc, _, _ := procCreateCompatibleDC.Call(0)
	defer procDeleteDC.Call(dc)
	bi := bitmapInfoHeader{Size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 32}
	buf := make([]byte, w*h*4)
	procGetDIBits.Call(dc, ii.HbmColor, 0, uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bi)), 0)
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	hasAlpha := false
	for i := 0; i < w*h; i++ {
		if buf[i*4+3] != 0 {
			hasAlpha = true
			break
		}
	}
	var mask []byte
	if !hasAlpha {
		mask = make([]byte, w*h*4)
		procGetDIBits.Call(dc, ii.HbmMask, 0, uintptr(h), uintptr(unsafe.Pointer(&mask[0])), uintptr(unsafe.Pointer(&bi)), 0)
	}
	for i := 0; i < w*h; i++ {
		b, g, r, a := buf[i*4], buf[i*4+1], buf[i*4+2], buf[i*4+3]
		if !hasAlpha {
			a = 255
			if mask[i*4] != 0 {
				a = 0
			}
		}
		img.Pix[i*4], img.Pix[i*4+1], img.Pix[i*4+2], img.Pix[i*4+3] = r, g, b, a
	}
	return img
}
