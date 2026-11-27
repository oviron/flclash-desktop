package winproxy

import (
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Setting is the proxy of the user's local network connection.
type Setting struct {
	// Flags are the INTERNET_PER_CONN_FLAGS: direct, a proxy, a script, detection.
	Flags         uint32 `json:"flags"`
	Server        string `json:"server,omitempty"`
	Bypass        string `json:"bypass,omitempty"`
	AutoConfigURL string `json:"autoConfigUrl,omitempty"`
}

const (
	optionPerConnection   = 75 // INTERNET_OPTION_PER_CONNECTION_OPTION
	optionSettingsChanged = 39 // INTERNET_OPTION_SETTINGS_CHANGED
	optionRefresh         = 37 // INTERNET_OPTION_REFRESH

	perConnFlags         = 1 // INTERNET_PER_CONN_FLAGS
	perConnProxyServer   = 2 // INTERNET_PER_CONN_PROXY_SERVER
	perConnProxyBypass   = 3 // INTERNET_PER_CONN_PROXY_BYPASS
	perConnAutoConfigURL = 4 // INTERNET_PER_CONN_AUTOCONFIG_URL

	// FlagDirect and FlagProxy are the PROXY_TYPE_ bits of Flags.
	FlagDirect = 1
	FlagProxy  = 2
)

// option is INTERNET_PER_CONN_OPTIONW: a DWORD and a union of a DWORD, a
// string pointer and a FILETIME, eight bytes aligned.
type option struct {
	kind  uint32
	_     uint32
	value uintptr
}

// optionList is INTERNET_PER_CONN_OPTION_LISTW.
type optionList struct {
	size        uint32
	connection  *uint16
	count       uint32
	optionError uint32
	options     *option
}

var (
	wininet          = windows.NewLazySystemDLL("wininet.dll")
	internetQueryOpt = wininet.NewProc("InternetQueryOptionW")
	internetSetOpt   = wininet.NewProc("InternetSetOptionW")
	kernel32         = windows.NewLazySystemDLL("kernel32.dll")
	globalFree       = kernel32.NewProc("GlobalFree")
)

// Read returns the current setting.
func Read() (Setting, error) {
	options := []option{{kind: perConnFlags}, {kind: perConnProxyServer}, {kind: perConnProxyBypass}, {kind: perConnAutoConfigURL}}
	list := optionList{count: uint32(len(options)), options: &options[0]}
	list.size = uint32(unsafe.Sizeof(list))
	size := list.size
	ok, _, err := internetQueryOpt.Call(0, optionPerConnection, uintptr(unsafe.Pointer(&list)), uintptr(unsafe.Pointer(&size)))
	if ok == 0 {
		return Setting{}, fmt.Errorf("InternetQueryOption: %w", err)
	}
	text := func(o option) string {
		if o.value == 0 {
			return ""
		}
		// The union holds a string WinINet allocated; read the field as the
		// pointer it is.
		s := windows.UTF16PtrToString(*(**uint16)(unsafe.Pointer(&o.value)))
		globalFree.Call(o.value)
		return s
	}
	return Setting{
		Flags:         uint32(options[0].value),
		Server:        text(options[1]),
		Bypass:        text(options[2]),
		AutoConfigURL: text(options[3]),
	}, nil
}

// Write replaces the setting and tells the programs of the session.
func Write(s Setting) error {
	options := []option{{kind: perConnFlags, value: uintptr(s.Flags)}}
	var keep [][]uint16
	add := func(kind uint32, value string) error {
		p, err := windows.UTF16FromString(value)
		if err != nil {
			return err
		}
		keep = append(keep, p)
		options = append(options, option{kind: kind, value: uintptr(unsafe.Pointer(&p[0]))})
		return nil
	}
	for _, o := range []struct {
		kind  uint32
		value string
	}{{perConnProxyServer, s.Server}, {perConnProxyBypass, s.Bypass}, {perConnAutoConfigURL, s.AutoConfigURL}} {
		if err := add(o.kind, o.value); err != nil {
			return err
		}
	}
	list := optionList{count: uint32(len(options)), options: &options[0]}
	list.size = uint32(unsafe.Sizeof(list))
	ok, _, err := internetSetOpt.Call(0, optionPerConnection, uintptr(unsafe.Pointer(&list)), uintptr(list.size))
	_ = keep
	if ok == 0 {
		return fmt.Errorf("InternetSetOption: %w", err)
	}
	internetSetOpt.Call(0, optionSettingsChanged, 0, 0)
	internetSetOpt.Call(0, optionRefresh, 0, 0)
	return nil
}

// Bypass is what must not go to the proxy whatever the policy says: the host
// itself and the local network, in the wildcards WinINet takes.
var Bypass = func() string {
	list := []string{"localhost", "127.*", "10.*", "192.168.*", "169.254.*", "*.local"}
	for second := 16; second <= 31; second++ {
		list = append(list, fmt.Sprintf("172.%d.*", second))
	}
	return strings.Join(append(list, "<local>"), ";")
}()

// Point sets the loopback port as the proxy of everything WinINet carries.
func Point(port int) error {
	return Write(Setting{Flags: FlagDirect | FlagProxy, Server: fmt.Sprintf("127.0.0.1:%d", port), Bypass: Bypass})
}
