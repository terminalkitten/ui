// 8 february 2014

package ui

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	stdWndClass = toUTF16("gouiwnd")
)

var (
	_defWindowProc = user32.NewProc("DefWindowProcW")
)

func defWindowProc(hwnd _HWND, uMsg uint32, wParam _WPARAM, lParam _LPARAM) _LRESULT {
	r1, _, _ := _defWindowProc.Call(
		uintptr(hwnd),
		uintptr(uMsg),
		uintptr(wParam),
		uintptr(lParam))
	return _LRESULT(r1)
}

// don't worry about error returns from GetWindowLongPtr()/SetWindowLongPtr()
// see comments of http://blogs.msdn.com/b/oldnewthing/archive/2014/02/03/10496248.aspx

func getWindowLongPtr(hwnd _HWND, what uintptr) uintptr {
	r1, _, _ := _getWindowLongPtr.Call(
		uintptr(hwnd),
		what)
	return r1
}

func setWindowLongPtr(hwnd _HWND, what uintptr, value uintptr) {
	_setWindowLongPtr.Call(
		uintptr(hwnd),
		what,
		value)
}

// we can store a pointer in extra space provided by Windows
// we'll store sysData there
// see http://blogs.msdn.com/b/oldnewthing/archive/2005/03/03/384285.aspx

func getSysData(hwnd _HWND) *sysData {
	return (*sysData)(unsafe.Pointer(getWindowLongPtr(hwnd, negConst(_GWLP_USERDATA))))
}

func storeSysData(hwnd _HWND, uMsg uint32, wParam _WPARAM, lParam _LPARAM) _LRESULT {
	// we can get the lpParam from CreateWindowEx() in WM_NCCREATE and WM_CREATE
	// we can freely skip any messages that come prior
	// see http://blogs.msdn.com/b/oldnewthing/archive/2005/04/22/410773.aspx and http://blogs.msdn.com/b/oldnewthing/archive/2014/02/03/10496248.aspx (note the date on the latter one!)
	if uMsg == _WM_NCCREATE {
		// the lpParam to CreateWindowEx() is the first uintptr of the CREATESTRUCT
		// so rather than create that whole structure, we'll just grab the uintptr at the address pointed to by lParam
		cs := (*uintptr)(unsafe.Pointer(lParam))
		saddr := *cs
		setWindowLongPtr(hwnd, negConst(_GWLP_USERDATA), saddr)
		// also set s.hwnd here so it can be used by other window messages right away
		s := (*sysData)(unsafe.Pointer(saddr))
		s.hwnd = hwnd
	}
	// then regardless of what happens, defer to DefWindowProc() (if you trace the execution of the above links, this is what they do)
	return defWindowProc(hwnd, uMsg, wParam, lParam)
}

func stdWndProc(hwnd _HWND, uMsg uint32, wParam _WPARAM, lParam _LPARAM) _LRESULT {
	s := getSysData(hwnd)
	if s == nil {		// not yet saved
		return storeSysData(hwnd, uMsg, wParam, lParam)
	}
	switch uMsg {
	case _WM_COMMAND:
		id := _HMENU(wParam.LOWORD())
		s.childrenLock.Lock()
		ss := s.children[id]
		s.childrenLock.Unlock()
		switch ss.ctype {
		case c_button:
			if wParam.HIWORD() == _BN_CLICKED {
				ss.signal()
			}
		case c_checkbox:
			// we opt into doing this ourselves because http://blogs.msdn.com/b/oldnewthing/archive/2014/05/22/10527522.aspx
			if wParam.HIWORD() == _BN_CLICKED {
				state, _, _ := _sendMessage.Call(
					uintptr(ss.hwnd),
					uintptr(_BM_GETCHECK),
					uintptr(0),
					uintptr(0))
				if state == _BST_CHECKED {
					state = _BST_UNCHECKED
				} else if state == _BST_UNCHECKED {
					state = _BST_CHECKED
				}
				_sendMessage.Call(
					uintptr(ss.hwnd),
					uintptr(_BM_SETCHECK),
					state,		// already uintptr
					uintptr(0))
			}
		}
		return 0
	case _WM_GETMINMAXINFO:
		mm := lParam.MINMAXINFO()
		// ... minimum size
		_ = mm
		return 0
	case _WM_SIZE:
		if s.resize != nil {
			var r _RECT

			r1, _, err := _getClientRect.Call(
				uintptr(hwnd),
				uintptr(unsafe.Pointer(&r)))
			if r1 == 0 {
				panic("GetClientRect failed: " + err.Error())
			}
			// top-left corner is (0,0) so no need for winheight
			s.doResize(int(r.left), int(r.top), int(r.right), int(r.bottom), 0)
			// TODO use the Defer movement functions here?
			// TODO redraw window and all children here?
		}
		return 0
	case _WM_CLOSE:
		s.signal()
		return 0
	default:
		return defWindowProc(hwnd, uMsg, wParam, lParam)
	}
	panic(fmt.Sprintf("stdWndProc message %d did not return: internal bug in ui library", uMsg))
}

type _WNDCLASS struct {
	style				uint32
	lpfnWndProc		uintptr
	cbClsExtra		int32		// originally int
	cbWndExtra		int32		// originally int
	hInstance			_HANDLE
	hIcon			_HANDLE
	hCursor			_HANDLE
	hbrBackground	_HBRUSH
	lpszMenuName	*uint16
	lpszClassName		uintptr
}

var (
	icon, cursor _HANDLE
)

var (
	_registerClass = user32.NewProc("RegisterClassW")
)

func registerStdWndClass() (err error) {
	wc := &_WNDCLASS{
		lpszClassName:	utf16ToArg(stdWndClass),
		lpfnWndProc:		syscall.NewCallback(stdWndProc),
		hInstance:		hInstance,
		hIcon:			icon,
		hCursor:			cursor,
		hbrBackground:	_HBRUSH(_COLOR_BTNFACE + 1),
	}
	r1, _, err := _registerClass.Call(uintptr(unsafe.Pointer(wc)))
	if r1 == 0 {		// failure
		return err
	}
	return nil
}

// no need to use/recreate MAKEINTRESOURCE() here as the Windows constant generator already took care of that because Microsoft's headers do already
func initWndClassInfo() (err error) {
	r1, _, err := user32.NewProc("LoadIconW").Call(
		uintptr(_NULL),
		uintptr(_IDI_APPLICATION))
	if r1 == 0 {		// failure
		return fmt.Errorf("error getting window icon: %v", err)
	}
	icon = _HANDLE(r1)

	r1, _, err = user32.NewProc("LoadCursorW").Call(
		uintptr(_NULL),
		uintptr(_IDC_ARROW))
	if r1 == 0 {		// failure
		return fmt.Errorf("error getting window cursor: %v", err)
	}
	cursor = _HANDLE(r1)

	return nil
}
