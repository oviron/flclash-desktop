// FlClash for Windows: the process in the notification area. It holds the
// engine and the model, keeps the icon and its menu, and starts the window,
// a program of its own, when it is asked for; the window lives only while it
// is open.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/tailscale/go-winio"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/oviron/flclash-desktop/internal/agent"
	"github.com/oviron/flclash-desktop/internal/app"
	"github.com/oviron/flclash-desktop/internal/helper"
	"github.com/oviron/flclash-desktop/internal/model"
	"github.com/oviron/flclash-desktop/internal/say"
	"github.com/oviron/flclash-desktop/internal/store"
)

// version is set by the build.
var version = "dev"

type host struct {
	sid   string
	pipe  string
	lang  string
	dir   string
	model *model.Model
	agent *agent.Agent
	tray  *tray

	mu     sync.Mutex
	window *os.Process

	hotKeyMu sync.Mutex
}

func main() {
	background := flag.Bool("background", false, "start in the notification area, without the window")
	page := flag.String("page", "", "the page to open: servers, routing, connections, log or settings")
	flag.Parse()

	sid, err := currentSID()
	if err != nil {
		messageBox("FlClash", err.Error())
		os.Exit(1)
	}
	pipe := `\\.\pipe\flclash-app-` + sid
	// One FlClash per person: a second start brings up the first.
	timeout := time.Second
	if conn, err := winio.DialPipe(pipe, &timeout); err == nil {
		pid, ok := ours(conn, sid)
		if !ok {
			conn.Close()
			messageBox("FlClash", say.In(language(), say.New("another program holds the address FlClash listens on")))
			os.Exit(1)
		}
		// This start has the right to bring a window forward; pass it on.
		procAllowSetForegroundWindow.Call(uintptr(pid))
		action := agent.Message{Action: "open", Page: *page}
		if *background {
			action = agent.Message{Action: "refresh"}
		}
		agent.Send(conn, action)
		return
	}
	listener, err := winio.ListenPipe(pipe, &winio.PipeConfig{
		// Only this person may talk to their FlClash.
		SecurityDescriptor: "D:P(A;;GA;;;" + sid + ")",
		InputBufferSize:    64 * 1024,
		OutputBufferSize:   256 * 1024,
	})
	if err != nil {
		messageBox("FlClash", err.Error())
		os.Exit(1)
	}
	exe, _ := os.Executable()
	procSetCurrentProcessExplicitAppID.Call(uintptr(unsafe.Pointer(utf16("FlClash"))))
	const perMonitorAwareV2 = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = -4
	procSetProcessDpiAwarenessContext.Call(perMonitorAwareV2)
	darkMenus()

	h := &host{sid: sid, pipe: pipe, lang: language(), dir: filepath.Dir(exe)}
	say.SetLanguage(h.lang)
	// The local port and the system proxy run here; the helper is for the
	// tunnel alone. Should this fail, every mode goes to the helper.
	if st, err := store.Open(); err == nil {
		_ = helper.RunLocal(filepath.Join(h.dir, app.CoreName), st.CoreDir())
	}
	h.model = model.New(h.lang)
	h.tray = &tray{host: h}
	h.agent = &agent.Agent{
		Model:         h.model,
		App:           h.app,
		Open:          h.open,
		InstallHelper: h.installHelper,
		SetHotKey:     h.setHotKey,
		SetLogin:      h.setLogin,
		Quit:          h.quit,
	}
	go h.agent.Serve(listener)
	h.model.Start()
	if !*background {
		go h.open(*page)
	}
	if err := h.tray.run(); err != nil {
		messageBox("FlClash", err.Error())
		os.Exit(1)
	}
}

func currentSID() (string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", err
	}
	return user.User.Sid.String(), nil
}

// language is the language of the system's interface, if the app speaks it.
func language() string {
	id, _, _ := procGetUserDefaultUILanguage.Call()
	// The primary language is the low ten bits; 0x19 is Russian.
	if id&0x3ff == 0x19 {
		return "ru"
	}
	return "en"
}

// open shows the window, starting it if it is not running.
func (h *host) open(page string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	// The window is another process; let it come to the front.
	if h.window != nil {
		procAllowSetForegroundWindow.Call(uintptr(h.window.Pid))
	}
	if h.agent.Activate(page) {
		return
	}
	// Started and not yet connected: it shows itself when it is.
	if h.window != nil {
		return
	}
	path := filepath.Join(h.dir, "window", "FlClash.Window.exe")
	args := []string{"--pipe", h.pipe}
	if page != "" {
		args = append(args, "--page", page)
	}
	cmd := exec.Command(path, args...)
	if err := cmd.Start(); err != nil {
		go messageBox("FlClash", say.In(h.lang, say.Errorf("the window did not open: %w", err)))
		return
	}
	h.window = cmd.Process
	procAllowSetForegroundWindow.Call(uintptr(cmd.Process.Pid))
	go func() {
		cmd.Wait()
		h.mu.Lock()
		h.window = nil
		h.mu.Unlock()
	}()
}

func (h *host) quit() {
	h.mu.Lock()
	if h.window != nil {
		h.window.Kill()
	}
	h.mu.Unlock()
	h.tray.remove()
	// Quitting turns the connection off, whatever the mode.
	h.model.Quit()
	os.Exit(0)
}

// prefs are the agent's own settings, beside the engine's.
type prefs struct {
	HotKey agent.HotKey `json:"hotKey"`
}

func prefsPath() string {
	base := os.Getenv("FLCLASH_HOME")
	if base == "" {
		base = filepath.Join(os.Getenv("LOCALAPPDATA"), "flclash-desktop")
	}
	return filepath.Join(base, "windows.json")
}

func (h *host) prefs() prefs {
	var p prefs
	if data, err := os.ReadFile(prefsPath()); err == nil {
		json.Unmarshal(data, &p)
	}
	return p
}

func (h *host) app() agent.App {
	return agent.App{Lang: h.lang, HotKey: h.prefs().HotKey, Login: h.login(), Version: version}
}

func (h *host) setHotKey(k agent.HotKey) error {
	// One change at a time: what is saved is what is registered.
	h.hotKeyMu.Lock()
	defer h.hotKeyMu.Unlock()
	if err := h.tray.setHotKey(k); err != nil {
		return errors.New(say.In(h.lang, err))
	}
	p := h.prefs()
	p.HotKey = k
	data, _ := json.MarshalIndent(p, "", "  ")
	os.MkdirAll(filepath.Dir(prefsPath()), 0o700)
	return os.WriteFile(prefsPath(), data, 0o600)
}

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func (h *host) startCommand() string {
	exe, _ := os.Executable()
	return `"` + exe + `" --background`
}

// login says whether FlClash starts with the system, from this place.
func (h *host) login() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	v, _, err := key.GetStringValue("FlClash")
	return err == nil && strings.EqualFold(v, h.startCommand())
}

func (h *host) setLogin(on bool) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	if on {
		return key.SetStringValue("FlClash", h.startCommand())
	}
	if err := key.DeleteValue("FlClash"); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

// installHelper runs the command line as administrator, through the
// system's prompt, and waits for it.
func (h *host) installHelper() error {
	const (
		seeMaskNoCloseProcess = 0x40
		seeMaskNoAsync        = 0x100
		swHide                = 0
		errorCancelled        = 1223
	)
	info := shellExecuteInfo{
		Mask:       seeMaskNoCloseProcess | seeMaskNoAsync,
		Verb:       utf16("runas"),
		File:       utf16(filepath.Join(h.dir, "flclashctl.exe")),
		Parameters: utf16("helper install -sid " + h.sid),
		Directory:  utf16(h.dir),
		Show:       swHide,
	}
	info.Size = uint32(unsafe.Sizeof(info))
	if r, _, err := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info))); r == 0 {
		if errno, ok := err.(windows.Errno); ok && errno == errorCancelled {
			return agent.ErrCancelled
		}
		return err
	}
	defer windows.CloseHandle(info.Process)
	windows.WaitForSingleObject(info.Process, windows.INFINITE)
	var code uint32
	windows.GetExitCodeProcess(info.Process, &code)
	// Run through the prompt, the command has no console to say why; the
	// person can run it by hand to read it.
	if code != 0 {
		return errors.New(say.In(h.lang, say.Errorf("the helper was not installed; to see why, run %s as administrator", `flclashctl helper install`)))
	}
	return nil
}

// ours says whether the other end of a pipe is a process of this user: the
// name of the pipe is known, and anyone could have taken it first.
func ours(conn net.Conn, sid string) (uint32, bool) {
	f, ok := conn.(interface{ Fd() uintptr })
	if !ok {
		return 0, false
	}
	var pid uint32
	if r, _, _ := procGetNamedPipeServerProcessId.Call(f.Fd(), uintptr(unsafe.Pointer(&pid))); r == 0 {
		return 0, false
	}
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return 0, false
	}
	defer windows.CloseHandle(process)
	var token windows.Token
	if windows.OpenProcessToken(process, windows.TOKEN_QUERY, &token) != nil {
		return 0, false
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	return pid, err == nil && user.User.Sid.String() == sid
}
