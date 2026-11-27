package main

import (
	"cmp"
	"runtime"
	"slices"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/oviron/flclash-desktop/internal/agent"
	"github.com/oviron/flclash-desktop/internal/app"
	"github.com/oviron/flclash-desktop/internal/bridge"
	"github.com/oviron/flclash-desktop/internal/model"
	"github.com/oviron/flclash-desktop/internal/say"
	"github.com/oviron/flclash-desktop/l10n"
)

// tray is the icon in the notification area and its menu, as the macOS menu
// bar item: the state at a glance, the choices people change most. A click
// opens the window; a right click opens the menu.
type tray struct {
	host *host
	hwnd windows.Handle
	// taskbarCreated comes when Explorer restarts; the icon is added again.
	taskbarCreated uint32
	icon           windows.Handle
	iconKey        string
	actions        []func()

	// hotKey is the shortcut registered now; only the window's thread
	// touches it.
	hotKey agent.HotKey
	// pending is the request on its way to that thread; the agent sends one
	// at a time.
	pending *hotKeyRequest
}

// hotKeyRequest carries a shortcut to the window's thread and the answer back.
type hotKeyRequest struct {
	want agent.HotKey
	err  error
}

var theTray *tray

func (t *tray) run() error {
	runtime.LockOSThread()
	theTray = t
	instance, _, _ := procGetModuleHandleW.Call(0)
	class := utf16("FlClashTray")
	wc := wndClassEx{
		WndProc:   windows.NewCallback(wndProc),
		Instance:  windows.Handle(instance),
		ClassName: class,
	}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return err
	}
	// A hidden top-level window: the menu needs one to belong to.
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(utf16("FlClash"))),
		0, 0, 0, 0, 0, 0, 0, instance, 0)
	if hwnd == 0 {
		return err
	}
	t.hwnd = windows.Handle(hwnd)
	t.taskbarCreated = registerMessage("TaskbarCreated")
	t.add()
	t.host.model.OnChange(func() { procPostMessageW.Call(uintptr(t.hwnd), wmRedraw, 0, 0) })
	t.registerHotKey(t.host.prefs().HotKey)
	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	t.remove()
	return nil
}

func registerMessage(name string) uint32 {
	r, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(utf16(name))))
	return uint32(r)
}

func wndProc(hwnd, message, wParam, lParam uintptr) uintptr {
	t := theTray
	switch {
	case t == nil:
	case uint32(message) == t.taskbarCreated && t.taskbarCreated != 0:
		t.add()
		return 0
	case message == wmTray:
		switch uint16(lParam) {
		case ninSelect, ninKeySelect:
			go t.host.open("")
		case wmContextMenu:
			t.menu(int32(int16(wParam)), int32(int16(wParam>>16)))
		}
		return 0
	case message == wmRedraw:
		t.update()
		return 0
	case message == wmOpenMenu:
		var p point
		procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
		t.menu(p.X, p.Y)
		return 0
	case message == wmSetHotKey:
		if req := t.pending; req != nil {
			req.err = t.registerHotKey(req.want)
		}
		return 0
	case message == wmHotKey:
		t.host.model.Toggle()
		return 0
	case message == wmSettingChange:
		// The taskbar may have turned light or dark.
		darkMenus()
		t.iconKey = ""
		t.update()
	case message == wmCommand:
		if id := int(uint16(wParam)); id > 0 && id <= len(t.actions) {
			action := t.actions[id-1]
			go action()
		}
		return 0
	case message == wmEndSession:
		// Signing out or shutting down ends the app as Quit does: the
		// connection goes, and the system proxy is put back while the
		// session can still be changed.
		if wParam != 0 {
			t.host.model.Quit()
		}
		return 0
	case message == wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, message, wParam, lParam)
	return r
}

func (t *tray) data() *notifyIconData {
	d := &notifyIconData{Wnd: t.hwnd, ID: 1}
	d.Size = uint32(unsafe.Sizeof(*d))
	return d
}

func (t *tray) add() {
	t.iconKey = ""
	d := t.data()
	d.Flags = nifMessage | nifIcon | nifTip | nifShowTip
	d.CallbackMessage = wmTray
	d.Icon, d.Tip = t.currentIcon()
	procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(d)))
	d.Version = 4
	procShellNotifyIconW.Call(nimSetVersion, uintptr(unsafe.Pointer(d)))
}

func (t *tray) remove() {
	procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(t.data())))
}

func (t *tray) update() {
	d := t.data()
	d.Flags = nifIcon | nifTip | nifShowTip
	d.Icon, d.Tip = t.currentIcon()
	procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(d)))
}

// currentIcon draws the icon for the state, once per state.
func (t *tray) currentIcon() (windows.Handle, [128]uint16) {
	s := t.host.model.Snapshot()
	lang := t.host.lang
	state := iconState(s)
	var tip [128]uint16
	copyTo(tip[:], "FlClash: "+headline(lang, s))
	light := lightTaskbar()
	key := state + map[bool]string{true: "/light", false: "/dark"}[light]
	if key != t.iconKey {
		size := smallIconSize()
		icon := newIcon(drawIcon(size, state, inkFor(light)))
		if t.icon != 0 {
			procDestroyIcon.Call(uintptr(t.icon))
		}
		t.icon, t.iconKey = icon, key
	}
	return t.icon, tip
}

func iconState(s model.Snapshot) string {
	switch {
	case s.Busy:
		return "busy"
	case s.HelperReady && s.Status.Blocked:
		return "blocked"
	case !s.HelperReady || (s.Status.Running && !s.Status.Captured):
		return "warning"
	case s.Status.Running:
		return "on"
	}
	return "off"
}

// headline is the state in a few words, as the macOS app says it.
func headline(lang string, s model.Snapshot) string {
	st := s.Status
	switch {
	// Only the tunnel needs the helper.
	case !s.HelperReady && !st.Installed:
		return l10n.T(lang, "Helper is not installed")
	case !s.HelperReady:
		return l10n.T(lang, "Helper needs an update")
	case st.Blocked && st.Mode == "system":
		return l10n.T(lang, "The proxy is down")
	case st.Blocked:
		return l10n.T(lang, "Internet is blocked")
	case !st.Running:
		return l10n.T(lang, "Off")
	case !st.Captured:
		return l10n.T(lang, "Not working")
	}
	return l10n.T(lang, "Connected")
}

// policyTitle names the built-in policies for people.
func policyTitle(lang, name string) string {
	switch name {
	case "all":
		return l10n.T(lang, "Everything through the server")
	case "lan-direct":
		return l10n.T(lang, "Local addresses direct")
	case "ru-direct":
		return l10n.T(lang, "Russia direct")
	}
	return name
}

type menuItem struct {
	title    string
	action   func()
	disabled bool
	checked  bool
	bold     bool
	sub      []menuItem
}

var separator = menuItem{}

// items is the menu for the state, as on macOS.
func (t *tray) items() []menuItem {
	h := t.host
	m := h.model
	s := m.Snapshot()
	lang := h.lang
	T := func(key string) string { return l10n.T(lang, key) }
	st := s.Status
	var selected bool
	for _, server := range s.View.Servers {
		selected = selected || server.ID == s.View.Selected
	}

	state := headline(lang, s)
	if st.Running && st.Server != "" {
		state += ": " + st.Server
	}
	items := []menuItem{{title: state, disabled: true}}
	toggle := T("Connect")
	if st.Running {
		toggle = T("Disconnect")
	} else if st.Blocked {
		toggle = T("Connect Again")
	}
	items = append(items, menuItem{title: toggle, action: m.Toggle, disabled: !s.HelperReady || s.Busy || !(st.Running || selected)})
	if s.HelperReady && st.Blocked {
		items = append(items, menuItem{title: T("Unblock"), action: m.TurnOff, disabled: s.Busy})
	}
	items = append(items, separator)

	var servers []menuItem
	groups := map[string]bool{}
	for _, server := range s.View.Servers {
		groups[server.Group] = true
	}
	ordered := slices.Clone(s.View.Servers)
	slices.SortStableFunc(ordered, func(a, b app.Server) int { return cmp.Compare(a.Group, b.Group) })
	last := "\x00"
	for _, server := range ordered {
		if len(groups) > 1 && server.Group != last {
			servers = append(servers, menuItem{title: server.Group, disabled: true, bold: true})
		}
		last = server.Group
		id := server.ID
		servers = append(servers, menuItem{title: server.Name, checked: id == s.View.Selected, action: func() { m.Change(selectServer(id), false, nil) }})
	}
	if len(servers) == 0 {
		servers = append(servers, menuItem{title: T("No servers yet"), disabled: true})
	}
	items = append(items, menuItem{title: T("Server"), sub: servers})

	var policies []menuItem
	for _, p := range s.View.Policies {
		name := p.Name
		policies = append(policies, menuItem{title: policyTitle(lang, name), checked: name == s.View.Policy, action: func() { m.Change(selectPolicy(name), false, nil) }})
	}
	items = append(items, menuItem{title: T("Routing"), sub: policies})

	var modes []menuItem
	for _, mode := range [][2]string{{"proxy", "Port Only"}, {"system", "System Proxy"}, {"tun", "TUN"}} {
		name := mode[0]
		modes = append(modes, menuItem{title: T(mode[1]), checked: name == s.View.Mode, action: func() {
			if name != m.Snapshot().View.Mode {
				// The tunnel installs the helper first, if it has to.
				h.agent.SetMode(name, nil)
			}
		}})
	}
	items = append(items, menuItem{title: T("Mode"), sub: modes, disabled: s.Busy}, separator)
	items = append(items,
		menuItem{title: T("Open FlClash"), action: func() { h.open("") }, bold: true},
		menuItem{title: T("Settings…"), action: func() { h.open("settings") }},
		separator,
		menuItem{title: T("Quit FlClash"), action: h.quit},
	)
	return items
}

func (t *tray) build(items []menuItem) uintptr {
	menu, _, _ := procCreatePopupMenu.Call()
	for _, item := range items {
		flags := uintptr(mfString)
		if item.title == "" {
			procAppendMenuW.Call(menu, mfSeparator, 0, 0)
			continue
		}
		if item.disabled {
			flags |= mfGrayed
		}
		if item.checked {
			flags |= mfChecked
		}
		if item.bold {
			flags |= mfDefault
		}
		id := uintptr(0)
		if item.sub != nil {
			flags |= mfPopup
			id = t.build(item.sub)
		} else if item.action != nil {
			t.actions = append(t.actions, item.action)
			id = uintptr(len(t.actions))
		}
		procAppendMenuW.Call(menu, flags, id, uintptr(unsafe.Pointer(utf16(item.title))))
	}
	return menu
}

func (t *tray) menu(x, y int32) {
	t.actions = nil
	menu := t.build(t.items())
	defer procDestroyMenu.Call(menu)
	// Without this the menu does not close when one clicks elsewhere.
	procSetForegroundWindow.Call(uintptr(t.hwnd))
	id, _, _ := procTrackPopupMenuEx.Call(menu, tpmReturnCmd|tpmRightButton|tpmBottomAlign, uintptr(x), uintptr(y), uintptr(t.hwnd), 0)
	procPostMessageW.Call(uintptr(t.hwnd), wmNull, 0, 0)
	if id > 0 && int(id) <= len(t.actions) {
		go t.actions[id-1]()
	}
}

// setHotKey registers the shortcut on the thread of the window, which is
// where the system delivers it.
func (t *tray) setHotKey(k agent.HotKey) error {
	req := &hotKeyRequest{want: k}
	t.pending = req
	// SendMessage waits until the window's thread has answered.
	procSendMessageW.Call(uintptr(t.hwnd), wmSetHotKey, 0, 0)
	t.pending = nil
	return req.err
}

// registerHotKey puts k in place of the shortcut there is; when k cannot be
// had, the old one stays.
func (t *tray) registerHotKey(k agent.HotKey) error {
	register := func(k agent.HotKey) bool {
		if k.Title == "" || k.Key == 0 {
			return true
		}
		r, _, _ := procRegisterHotKey.Call(uintptr(t.hwnd), 1, uintptr(k.Modifiers|modNoRepeat), uintptr(k.Key))
		return r != 0
	}
	procUnregisterHotKey.Call(uintptr(t.hwnd), 1)
	if !register(k) {
		register(t.hotKey)
		return say.New("the shortcut is taken by another program")
	}
	t.hotKey = k
	return nil
}

func selectServer(id string) bridge.Request   { return bridge.Request{Op: "select", Server: id} }
func selectPolicy(name string) bridge.Request { return bridge.Request{Op: "select", Policy: name} }
