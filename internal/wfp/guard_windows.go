package wfp

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

type wfpObjectInstaller func(uintptr) error

// baseObjects are the provider and the sublayer the filters of one session
// belong to.
type baseObjects struct {
	provider windows.GUID
	filters  windows.GUID
}

// Rules say what may leave the host.
type Rules struct {
	// Core is the path of the program that carries the tunnel: its own
	// connections go out past the tunnel, to the server.
	Core string
	// TunLUID is the tunnel device; zero when it is gone.
	TunLUID uint64
	// Full closes IPv4 too: what does not go into the device, come from the
	// core or stay in the local network is refused, and names are resolved
	// nowhere but inside the tunnel. Without it only IPv6 is closed.
	Full bool
	// LocalV4 and LocalV6 are where traffic may go without the tunnel.
	LocalV4 []netip.Prefix
	LocalV6 []netip.Prefix
}

// Guard holds one set of rules at a time.
type Guard struct {
	mu      sync.Mutex
	session uintptr
	full    bool
}

// Apply replaces the rules. The new ones are in place before the old ones go,
// so there is no moment with neither.
func (g *Guard) Apply(r Rules) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	session, err := openSession()
	if err != nil {
		return err
	}
	if err := runTransaction(session, func(session uintptr) error { return install(session, r) }); err != nil {
		fwpmEngineClose0(session)
		return err
	}
	if g.session != 0 {
		fwpmEngineClose0(g.session)
	}
	g.session, g.full = session, r.Full
	return nil
}

// Release removes the rules.
func (g *Guard) Release() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.session != 0 {
		fwpmEngineClose0(g.session)
	}
	g.session, g.full = 0, false
}

// Holding reports whether the full rules are in place.
func (g *Guard) Holding() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.session != 0 && g.full
}

// InterfaceLUID finds the LUID of a network interface by its index.
func InterfaceLUID(index int) (uint64, error) {
	var luid uint64
	ret, _, _ := convertIndexToLuid.Call(uintptr(index), uintptr(unsafe.Pointer(&luid)))
	if ret != 0 {
		return 0, fmt.Errorf("ConvertInterfaceIndexToLuid: %w", windows.Errno(ret))
	}
	return luid, nil
}

var convertIndexToLuid = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("ConvertInterfaceIndexToLuid")

// openSession opens a dynamic session: what it adds goes away with it, and
// with this process.
func openSession() (uintptr, error) {
	display, err := createWtFwpmDisplayData0("FlClash", "FlClash guard")
	if err != nil {
		return 0, err
	}
	session := wtFwpmSession0{
		displayData:          *display,
		flags:                cFWPM_SESSION_FLAG_DYNAMIC,
		txnWaitTimeoutInMSec: windows.INFINITE,
	}
	handle := uintptr(0)
	if err := fwpmEngineOpen0(nil, cRPC_C_AUTHN_WINNT, nil, &session, unsafe.Pointer(&handle)); err != nil {
		return 0, wrapErr(err)
	}
	return handle, nil
}

func install(session uintptr, r Rules) error {
	bo := &baseObjects{}
	var err error
	if bo.provider, err = windows.GenerateGUID(); err != nil {
		return err
	}
	if bo.filters, err = windows.GenerateGUID(); err != nil {
		return err
	}
	display, err := createWtFwpmDisplayData0("FlClash", "FlClash guard")
	if err != nil {
		return err
	}
	if err := fwpmProviderAdd0(session, &wtFwpmProvider0{providerKey: bo.provider, displayData: *display}, 0); err != nil {
		return wrapErr(err)
	}
	sublayer := wtFwpmSublayer0{subLayerKey: bo.filters, displayData: *display, providerKey: &bo.provider, weight: ^uint16(0)}
	if err := fwpmSubLayerAdd0(session, &sublayer, 0); err != nil {
		return wrapErr(err)
	}

	layers := []windows.GUID{cFWPM_LAYER_ALE_AUTH_CONNECT_V6}
	if r.Full {
		layers = append(layers, cFWPM_LAYER_ALE_AUTH_CONNECT_V4)
	}

	// The core goes anywhere: it carries the tunnel.
	core, err := appID(r.Core)
	if err != nil {
		return fmt.Errorf("the core %s: %w", r.Core, err)
	}
	defer fwpmFreeMemory0(unsafe.Pointer(&core))
	coreCondition := []wtFwpmFilterCondition0{{
		fieldKey:       cFWPM_CONDITION_ALE_APP_ID,
		matchType:      cFWP_MATCH_EQUAL,
		conditionValue: wtFwpConditionValue0{_type: cFWP_BYTE_BLOB_TYPE, value: uintptr(unsafe.Pointer(core))},
	}}
	if err := add(session, bo, "Permit the core", layers, 15, cFWP_ACTION_PERMIT, coreCondition); err != nil {
		return err
	}
	if err := permitLoopback(session, bo, 15); err != nil {
		return err
	}
	if r.TunLUID != 0 {
		luid := r.TunLUID
		device := []wtFwpmFilterCondition0{{
			fieldKey:       cFWPM_CONDITION_IP_LOCAL_INTERFACE,
			matchType:      cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{_type: cFWP_UINT64, value: uintptr(unsafe.Pointer(&luid))},
		}}
		if err := add(session, bo, "Permit the tunnel device", layers, 15, cFWP_ACTION_PERMIT, device); err != nil {
			return err
		}
		runtime.KeepAlive(&luid)
	}

	if r.Full {
		// Names are resolved inside the tunnel or by the core, never by the
		// resolver of the local network: a name is a leak too.
		dns := []wtFwpmFilterCondition0{
			{fieldKey: cFWPM_CONDITION_IP_REMOTE_PORT, matchType: cFWP_MATCH_EQUAL, conditionValue: wtFwpConditionValue0{_type: cFWP_UINT16, value: 53}},
			{fieldKey: cFWPM_CONDITION_IP_REMOTE_PORT, matchType: cFWP_MATCH_EQUAL, conditionValue: wtFwpConditionValue0{_type: cFWP_UINT16, value: 853}},
			{fieldKey: cFWPM_CONDITION_IP_PROTOCOL, matchType: cFWP_MATCH_EQUAL, conditionValue: wtFwpConditionValue0{_type: cFWP_UINT8, value: uintptr(cIPPROTO_UDP)}},
			{fieldKey: cFWPM_CONDITION_IP_PROTOCOL, matchType: cFWP_MATCH_EQUAL, conditionValue: wtFwpConditionValue0{_type: cFWP_UINT8, value: uintptr(cIPPROTO_TCP)}},
		}
		if err := add(session, bo, "Block DNS past the tunnel", layers, 14, cFWP_ACTION_BLOCK, dns); err != nil {
			return err
		}
		var v4 []wtFwpV4AddrAndMask
		for _, p := range r.LocalV4 {
			if !p.Addr().Is4() {
				continue
			}
			v4 = append(v4, wtFwpV4AddrAndMask{
				addr: binary.BigEndian.Uint32(p.Masked().Addr().AsSlice()),
				mask: ^uint32(0) << (32 - p.Bits()),
			})
		}
		if len(v4) > 0 {
			conditions := make([]wtFwpmFilterCondition0, len(v4))
			for i := range v4 {
				conditions[i] = wtFwpmFilterCondition0{
					fieldKey:       cFWPM_CONDITION_IP_REMOTE_ADDRESS,
					matchType:      cFWP_MATCH_EQUAL,
					conditionValue: wtFwpConditionValue0{_type: cFWP_V4_ADDR_MASK, value: uintptr(unsafe.Pointer(&v4[i]))},
				}
			}
			if err := add(session, bo, "Permit the local network (IPv4)", []windows.GUID{cFWPM_LAYER_ALE_AUTH_CONNECT_V4}, 13, cFWP_ACTION_PERMIT, conditions); err != nil {
				return err
			}
			runtime.KeepAlive(v4)
		}
		if err := permitDHCPIPv4(session, bo, 13); err != nil {
			return err
		}
	}

	var v6 []wtFwpV6AddrAndMask
	for _, p := range r.LocalV6 {
		if !p.Addr().Is6() {
			continue
		}
		v6 = append(v6, wtFwpV6AddrAndMask{addr: p.Masked().Addr().As16(), prefixLength: uint8(p.Bits())})
	}
	if len(v6) > 0 {
		conditions := make([]wtFwpmFilterCondition0, len(v6))
		for i := range v6 {
			conditions[i] = wtFwpmFilterCondition0{
				fieldKey:       cFWPM_CONDITION_IP_REMOTE_ADDRESS,
				matchType:      cFWP_MATCH_EQUAL,
				conditionValue: wtFwpConditionValue0{_type: cFWP_V6_ADDR_MASK, value: uintptr(unsafe.Pointer(&v6[i]))},
			}
		}
		if err := add(session, bo, "Permit the local network (IPv6)", []windows.GUID{cFWPM_LAYER_ALE_AUTH_CONNECT_V6}, 13, cFWP_ACTION_PERMIT, conditions); err != nil {
			return err
		}
		runtime.KeepAlive(v6)
	}
	if err := permitDHCPIPv6(session, bo, 13); err != nil {
		return err
	}
	if err := permitNdp(session, bo, 13); err != nil {
		return err
	}

	// The rest is refused: IPv6 always, IPv4 under the full guard.
	return add(session, bo, "Block the rest", layers, 0, cFWP_ACTION_BLOCK, nil)
}

func add(session uintptr, bo *baseObjects, name string, layers []windows.GUID, weight uint8, action wtFwpActionType, conditions []wtFwpmFilterCondition0) error {
	display, err := createWtFwpmDisplayData0(name, "")
	if err != nil {
		return err
	}
	filter := wtFwpmFilter0{
		displayData: *display,
		providerKey: &bo.provider,
		subLayerKey: bo.filters,
		weight:      filterWeight(weight),
		action:      wtFwpmAction0{_type: action},
	}
	if len(conditions) > 0 {
		filter.numFilterConditions = uint32(len(conditions))
		filter.filterCondition = &conditions[0]
	}
	for _, layer := range layers {
		filter.layerKey = layer
		id := uint64(0)
		if err := fwpmFilterAdd0(session, &filter, 0, &id); err != nil {
			return wrapErr(fmt.Errorf("%s: %w", name, err))
		}
	}
	runtime.KeepAlive(conditions)
	return nil
}
