//go:build windows

package winapi

import (
	"runtime"
	"sync"
	"unsafe"
)

// runHook installs the low-level hook id, calling cb, on a thread of its
// own whose message loop does nothing else, and returns the function that
// removes it. Every low-level hook of Stage Manager starts and stops this
// one way: on the main thread, anything that kept it busy would hold up
// the keyboard or mouse of the whole desktop, and Windows drops a hook
// that keeps input waiting longer than LowLevelHooksTimeout.
//
// arm publishes the state the callback reads, before the hook can call it;
// disarm takes it back. disarm runs as unhook is called, so that the
// callback acts no more from then on, and again once the hook is gone,
// for state the callback may have set meanwhile; it must be safe to run
// twice. unhook waits for the thread to end, and does nothing the second
// time: called on, it would post to a thread id Windows may have given to
// another thread of the process. Call it outside the callback.
func runHook(id, cb uintptr, arm, disarm func()) (unhook func(), err error) {
	type started struct {
		tid uint32
		err error
	}
	ready := make(chan started)
	stopped := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(stopped)
		var m threadMsg
		// The thread gets its message queue here, for the quit to reach.
		procPeekMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, pmNoRemove)
		mod, _, _ := procGetModuleHandleW.Call(0)
		arm()
		h, _, e := procSetWindowsHookExW.Call(id, cb, mod, 0)
		if h == 0 {
			disarm()
			ready <- started{err: e}
			return
		}
		ready <- started{tid: CurrentThreadID()}
		for {
			if r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0); int32(r) <= 0 {
				break
			}
		}
		procUnhookWindowsHookEx.Call(h)
		disarm()
	}()
	s := <-ready
	if s.err != nil {
		return nil, s.err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			disarm()
			procPostThreadMessageW.Call(uintptr(s.tid), wmQuit, 0, 0)
			<-stopped
		})
	}, nil
}
