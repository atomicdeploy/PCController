//go:build windows

package comserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
	"pccontroller.local/desktop/internal/ipc"
)

const (
	coinitApartmentThreaded = 0x2
	clsctxLocalServer       = 0x4
	regclsMultipleUse       = 0x1

	pmNoRemove = 0x0000
	wmQuit     = 0x0012

	sOK                    uint32 = 0x00000000
	eNoInterface           uint32 = 0x80004002
	ePointer               uint32 = 0x80004003
	eFail                  uint32 = 0x80004005
	eOutOfMemory           uint32 = 0x8007000E
	eInvalidArg            uint32 = 0x80070057
	eUnexpected            uint32 = 0x8000FFFF
	classENoAggregation    uint32 = 0x80040110
	dispEUnknownInterface  uint32 = 0x80020001
	dispEMemberNotFound    uint32 = 0x80020003
	dispETypeMismatch      uint32 = 0x80020005
	dispEUnknownName       uint32 = 0x80020006
	dispEException         uint32 = 0x80020009
	dispEBadIndex          uint32 = 0x8002000B
	dispEBadParameterCount uint32 = 0x8002000E

	vtEmpty = 0
	vtI2    = 2
	vtI4    = 3
	vtR4    = 4
	vtR8    = 5
	vtBSTR  = 8
	vtBool  = 11
	vtI8    = 20
	vtUI8   = 21

	maxAutomationNames = 32
	maxBSTRCodeUnits   = 1 << 20
	codePageUTF8       = 65001
)

var (
	ole32                   = windows.NewLazySystemDLL("ole32.dll")
	oleaut32                = windows.NewLazySystemDLL("oleaut32.dll")
	kernel32                = windows.NewLazySystemDLL("kernel32.dll")
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procCoInitializeEx      = ole32.NewProc("CoInitializeEx")
	procCoUninitialize      = ole32.NewProc("CoUninitialize")
	procCoRegisterClass     = ole32.NewProc("CoRegisterClassObject")
	procCoRevokeClass       = ole32.NewProc("CoRevokeClassObject")
	procSysAllocStringLen   = oleaut32.NewProc("SysAllocStringLen")
	procSysFreeString       = oleaut32.NewProc("SysFreeString")
	procSysStringLen        = oleaut32.NewProc("SysStringLen")
	procVariantChangeType   = oleaut32.NewProc("VariantChangeType")
	procVariantClear        = oleaut32.NewProc("VariantClear")
	procWideCharToMultiByte = kernel32.NewProc("WideCharToMultiByte")
	procPeekMessage         = user32.NewProc("PeekMessageW")
	procGetMessage          = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessage     = user32.NewProc("DispatchMessageW")
	procPostThreadMessage   = user32.NewProc("PostThreadMessageW")

	iidIUnknown = windows.GUID{
		Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46},
	}
	iidIClassFactory = windows.GUID{
		Data1: 1,
		Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46},
	}
	iidIDispatch = windows.GUID{
		Data1: 0x00020400,
		Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46},
	}
	iidNull windows.GUID
	classID = windows.GUID{
		Data1: 0xD9603C3B,
		Data2: 0x7D90,
		Data3: 0x53B2,
		Data4: [8]byte{0xAC, 0x01, 0x76, 0x56, 0xB0, 0x27, 0x60, 0x3F},
	}

	nativeTables struct {
		once            sync.Once
		factory         uintptr
		dispatch        uintptr
		err             error
		factoryEntries  [5]uintptr
		dispatchEntries [7]uintptr
	}
	factoryStates  sync.Map
	dispatchStates sync.Map
)

type nativeServer struct {
	ctx      context.Context
	bridge   *bridge
	threadID uint32

	objects    atomic.Int64
	locks      atomic.Int64
	seenClient atomic.Bool
	quitting   atomic.Bool
}

type factoryState struct {
	refs     atomic.Uint32
	server   *nativeServer
	identity *comIdentity
}

type dispatchState struct {
	refs     atomic.Uint32
	server   *nativeServer
	identity *comIdentity
}

type comIdentity struct {
	cell   *uintptr
	pinner runtime.Pinner
}

type variant struct {
	VT        uint16
	Reserved1 uint16
	Reserved2 uint16
	Reserved3 uint16
	Value     uint64
}

type dispatchParams struct {
	Args          *variant
	NamedArgIDs   *int32
	ArgCount      uint32
	NamedArgCount uint32
}

type exceptionInfo struct {
	Code            uint16
	Reserved        uint16
	Source          uintptr
	Description     uintptr
	HelpFile        uintptr
	HelpContext     uint32
	ReservedPointer uintptr
	DeferredFillIn  uintptr
	ResultCode      int32
}

type point struct {
	X int32
	Y int32
}

type windowMessage struct {
	Window  uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Point   point
	Private uint32
}

type hresultError struct {
	operation string
	code      uint32
}

func (e hresultError) Error() string {
	return fmt.Sprintf("comserver: %s failed with HRESULT 0x%08X", e.operation, e.code)
}

// Run registers PCControllerDesktop's class factory for the lifetime of ctx and pumps a
// single-threaded COM apartment on a locked OS thread. It also exits after the
// last activated object and LockServer reference are released.
func Run(ctx context.Context, handler ipc.Handler) error {
	automationBridge, err := newBridge(handler)
	if err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := initializeNativeTables(); err != nil {
		return err
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	result, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded)
	if hresultFailed(result) {
		return hresultError{operation: "CoInitializeEx", code: uint32(result)}
	}
	defer procCoUninitialize.Call()

	server := &nativeServer{
		ctx:      ctx,
		bridge:   automationBridge,
		threadID: windows.GetCurrentThreadId(),
	}

	// Calling PeekMessage creates this thread's message queue before another
	// goroutine can post WM_QUIT in response to cancellation.
	var message windowMessage
	procPeekMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0, pmNoRemove)

	factory, err := newFactory(server)
	if err != nil {
		return err
	}
	factoryOwned := true
	defer func() {
		if factoryOwned {
			factoryRelease(factory)
		}
	}()

	var cookie uint32
	result, _, _ = procCoRegisterClass.Call(
		uintptr(unsafe.Pointer(&classID)),
		factory,
		clsctxLocalServer,
		regclsMultipleUse,
		uintptr(unsafe.Pointer(&cookie)),
	)
	if hresultFailed(result) {
		return hresultError{operation: "CoRegisterClassObject", code: uint32(result)}
	}

	stopWatcher := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-ctx.Done():
			server.requestQuit()
		case <-stopWatcher:
		}
	}()

	loopErr := runMessageLoop(&message)
	close(stopWatcher)
	<-watcherDone

	revokeResult, _, _ := procCoRevokeClass.Call(uintptr(cookie))
	factoryRelease(factory)
	factoryOwned = false

	if loopErr != nil {
		return loopErr
	}
	if hresultFailed(revokeResult) {
		return hresultError{operation: "CoRevokeClassObject", code: uint32(revokeResult)}
	}
	return nil
}

func runMessageLoop(message *windowMessage) error {
	for {
		result, _, callErr := procGetMessage.Call(
			uintptr(unsafe.Pointer(message)),
			0,
			0,
			0,
		)
		switch int32(uint32(result)) {
		case -1:
			if callErr != nil && callErr != windows.ERROR_SUCCESS {
				return fmt.Errorf("comserver: GetMessageW: %w", callErr)
			}
			return errors.New("comserver: GetMessageW failed")
		case 0:
			return nil
		default:
			procTranslateMessage.Call(uintptr(unsafe.Pointer(message)))
			procDispatchMessage.Call(uintptr(unsafe.Pointer(message)))
		}
	}
}

func (s *nativeServer) requestQuit() {
	if !s.quitting.CompareAndSwap(false, true) {
		return
	}
	procPostThreadMessage.Call(uintptr(s.threadID), wmQuit, 0, 0)
}

func (s *nativeServer) maybeQuit() {
	if s.seenClient.Load() && s.objects.Load() == 0 && s.locks.Load() == 0 {
		s.requestQuit()
	}
}

func (s *nativeServer) releaseLock() {
	for {
		current := s.locks.Load()
		if current <= 0 {
			break
		}
		if s.locks.CompareAndSwap(current, current-1) {
			break
		}
	}
	s.maybeQuit()
}

func initializeNativeTables() error {
	nativeTables.once.Do(func() {
		nativeTables.factoryEntries = [5]uintptr{
			windows.NewCallback(factoryQueryInterface),
			windows.NewCallback(factoryAddRef),
			windows.NewCallback(factoryRelease),
			windows.NewCallback(factoryCreateInstance),
			windows.NewCallback(factoryLockServer),
		}
		nativeTables.dispatchEntries = [7]uintptr{
			windows.NewCallback(dispatchQueryInterface),
			windows.NewCallback(dispatchAddRef),
			windows.NewCallback(dispatchRelease),
			windows.NewCallback(dispatchGetTypeInfoCount),
			windows.NewCallback(dispatchGetTypeInfo),
			windows.NewCallback(dispatchGetIDsOfNames),
			windows.NewCallback(dispatchInvoke),
		}
		// The arrays live in static global storage for the process lifetime.
		nativeTables.factory = uintptr(unsafe.Pointer(&nativeTables.factoryEntries[0]))
		nativeTables.dispatch = uintptr(unsafe.Pointer(&nativeTables.dispatchEntries[0]))
	})
	return nativeTables.err
}

func newIdentity(vtable uintptr) *comIdentity {
	cell := new(uintptr)
	*cell = vtable
	identity := &comIdentity{cell: cell}
	identity.pinner.Pin(cell)
	return identity
}

func (i *comIdentity) address() uintptr {
	if i == nil || i.cell == nil {
		return 0
	}
	return uintptr(unsafe.Pointer(i.cell))
}

func (i *comIdentity) free() {
	if i == nil || i.cell == nil {
		return
	}
	i.pinner.Unpin()
	i.cell = nil
}

func newFactory(server *nativeServer) (uintptr, error) {
	identity := newIdentity(nativeTables.factory)
	state := &factoryState{server: server, identity: identity}
	state.refs.Store(1)
	address := identity.address()
	factoryStates.Store(address, state)
	return address, nil
}

func newDispatch(server *nativeServer) (uintptr, error) {
	identity := newIdentity(nativeTables.dispatch)
	state := &dispatchState{server: server, identity: identity}
	state.refs.Store(1)
	address := identity.address()
	dispatchStates.Store(address, state)
	server.objects.Add(1)
	return address, nil
}

func factoryQueryInterface(this uintptr, iidPointer *windows.GUID, resultPointer *uintptr) uintptr {
	if resultPointer == nil {
		return hresult(ePointer)
	}
	*resultPointer = 0
	state, ok := loadFactory(this)
	if !ok {
		return hresult(eUnexpected)
	}
	if !guidMatches(iidPointer, iidIUnknown) && !guidMatches(iidPointer, iidIClassFactory) {
		return hresult(eNoInterface)
	}
	if addReference(&state.refs) == 0 {
		return hresult(eUnexpected)
	}
	*resultPointer = this
	return hresult(sOK)
}

func factoryAddRef(this uintptr) uintptr {
	state, ok := loadFactory(this)
	if !ok {
		return 0
	}
	return uintptr(addReference(&state.refs))
}

func factoryRelease(this uintptr) uintptr {
	state, ok := loadFactory(this)
	if !ok {
		return 0
	}
	remaining := releaseReference(&state.refs)
	if remaining == 0 {
		factoryStates.Delete(this)
		state.identity.free()
	}
	return uintptr(remaining)
}

func factoryCreateInstance(this, outer uintptr, iidPointer *windows.GUID, resultPointer *uintptr) uintptr {
	if resultPointer == nil {
		return hresult(ePointer)
	}
	*resultPointer = 0
	if outer != 0 {
		return hresult(classENoAggregation)
	}
	state, ok := loadFactory(this)
	if !ok {
		return hresult(eUnexpected)
	}
	identity, err := newDispatch(state.server)
	if err != nil {
		return hresult(eOutOfMemory)
	}
	result := dispatchQueryInterface(identity, iidPointer, resultPointer)
	if !hresultFailed(result) {
		state.server.seenClient.Store(true)
	}
	dispatchRelease(identity)
	return result
}

func factoryLockServer(this, lock uintptr) uintptr {
	state, ok := loadFactory(this)
	if !ok {
		return hresult(eUnexpected)
	}
	if lock != 0 {
		state.server.seenClient.Store(true)
		state.server.locks.Add(1)
	} else {
		state.server.releaseLock()
	}
	return hresult(sOK)
}

func dispatchQueryInterface(this uintptr, iidPointer *windows.GUID, resultPointer *uintptr) uintptr {
	if resultPointer == nil {
		return hresult(ePointer)
	}
	*resultPointer = 0
	state, ok := loadDispatch(this)
	if !ok {
		return hresult(eUnexpected)
	}
	if !guidMatches(iidPointer, iidIUnknown) && !guidMatches(iidPointer, iidIDispatch) {
		return hresult(eNoInterface)
	}
	if addReference(&state.refs) == 0 {
		return hresult(eUnexpected)
	}
	*resultPointer = this
	return hresult(sOK)
}

func dispatchAddRef(this uintptr) uintptr {
	state, ok := loadDispatch(this)
	if !ok {
		return 0
	}
	return uintptr(addReference(&state.refs))
}

func dispatchRelease(this uintptr) uintptr {
	state, ok := loadDispatch(this)
	if !ok {
		return 0
	}
	remaining := releaseReference(&state.refs)
	if remaining == 0 {
		dispatchStates.Delete(this)
		state.identity.free()
		state.server.objects.Add(-1)
		state.server.maybeQuit()
	}
	return uintptr(remaining)
}

func dispatchGetTypeInfoCount(this uintptr, countPointer *uint32) uintptr {
	if _, ok := loadDispatch(this); !ok {
		return hresult(eUnexpected)
	}
	if countPointer == nil {
		return hresult(ePointer)
	}
	*countPointer = 0
	return hresult(sOK)
}

func dispatchGetTypeInfo(this, index, _ /* lcid */ uintptr, infoPointer *uintptr) uintptr {
	if _, ok := loadDispatch(this); !ok {
		return hresult(eUnexpected)
	}
	if infoPointer == nil {
		return hresult(ePointer)
	}
	*infoPointer = 0
	if index != 0 {
		return hresult(dispEBadIndex)
	}
	return hresult(dispEBadIndex)
}

func dispatchGetIDsOfNames(
	this uintptr,
	iidPointer *windows.GUID,
	namesPointer **uint16,
	count,
	_ /* lcid */ uintptr,
	idsPointer *int32,
) (result uintptr) {
	defer func() {
		if recover() != nil {
			result = hresult(eInvalidArg)
		}
	}()
	if _, ok := loadDispatch(this); !ok {
		return hresult(eUnexpected)
	}
	if !guidMatches(iidPointer, iidNull) {
		return hresult(dispEUnknownInterface)
	}
	if namesPointer == nil || idsPointer == nil {
		return hresult(ePointer)
	}
	if count == 0 || count > maxAutomationNames {
		return hresult(eInvalidArg)
	}

	namePointers := unsafe.Slice(namesPointer, int(count))
	ids := unsafe.Slice(idsPointer, int(count))
	unknown := false
	for index := range ids {
		ids[index] = -1
		if namePointers[index] == nil {
			unknown = true
			continue
		}
		name := windows.UTF16PtrToString(namePointers[index])
		id, ok := resolveName(name)
		if !ok || (index > 0 && id != dispIDMessage) {
			unknown = true
			continue
		}
		ids[index] = id
	}
	if unknown {
		return hresult(dispEUnknownName)
	}
	return hresult(sOK)
}

func dispatchInvoke(
	this,
	dispIDValue uintptr,
	iidPointer *windows.GUID,
	_, /* lcid */
	flags uintptr,
	params *dispatchParams,
	resultValue *variant,
	exception *exceptionInfo,
	argError *uint32,
) (result uintptr) {
	defer func() {
		if recovered := recover(); recovered != nil {
			fillException(exception, fmt.Errorf("internal Automation panic: %v", recovered))
			result = hresult(dispEException)
		}
	}()
	state, ok := loadDispatch(this)
	if !ok {
		return hresult(eUnexpected)
	}
	if !guidMatches(iidPointer, iidNull) {
		return hresult(dispEUnknownInterface)
	}
	if resultValue != nil {
		*resultValue = variant{}
	}
	if exception != nil {
		*exception = exceptionInfo{}
	}
	if argError != nil {
		*argError = 0
	}

	dispID := int32(uint32(dispIDValue))
	expected, known := expectedArgumentCount(dispID)
	if !known {
		return hresult(dispEMemberNotFound)
	}
	count := uint32(0)
	if params != nil {
		count = params.ArgCount
	}
	if count != uint32(expected) {
		return hresult(dispEBadParameterCount)
	}

	var args []any
	if count > 0 {
		if params.Args == nil {
			return hresult(ePointer)
		}
		nativeArgs := unsafe.Slice(params.Args, int(count))
		args = make([]any, count)
		for index := range nativeArgs {
			automationIndex := len(nativeArgs) - 1 - index
			value, err := variantAsString(&nativeArgs[index])
			if err != nil {
				if argError != nil {
					*argError = uint32(index)
				}
				return hresult(dispETypeMismatch)
			}
			args[automationIndex] = value
		}
	}

	commandContext, cancel := context.WithTimeout(state.server.ctx, 12*time.Second)
	defer cancel()
	value, err := state.server.bridge.invoke(commandContext, dispID, uint16(flags), args)
	if err != nil {
		switch {
		case errors.Is(err, errUnknownMember), errors.Is(err, errMemberAccess):
			return hresult(dispEMemberNotFound)
		case errors.Is(err, errBadParameterCount):
			return hresult(dispEBadParameterCount)
		case errors.Is(err, errTypeMismatch):
			return hresult(dispETypeMismatch)
		default:
			fillException(exception, err)
			return hresult(dispEException)
		}
	}
	if resultValue == nil {
		return hresult(sOK)
	}
	if code := setVariant(resultValue, value); code != sOK {
		fillException(exception, errors.New("unable to encode Automation result"))
		return hresult(code)
	}
	return hresult(sOK)
}

func expectedArgumentCount(dispID int32) (int, bool) {
	switch dispID {
	case dispIDOn, dispIDOff, dispIDToggle, dispIDShow, dispIDReconnect,
		dispIDGetState, dispIDState, dispIDHide:
		return 0, true
	case dispIDNotify:
		return 1, true
	default:
		return 0, false
	}
}

func setVariant(destination *variant, value any) uint32 {
	*destination = variant{}
	switch typed := value.(type) {
	case nil:
		destination.VT = vtEmpty
	case string:
		return setVariantString(destination, typed)
	case []byte:
		return setVariantString(destination, string(typed))
	case json.RawMessage:
		return setVariantString(destination, string(typed))
	case bool:
		destination.VT = vtBool
		if typed {
			destination.Value = 0xFFFF
		}
	case int:
		if typed >= math.MinInt32 && typed <= math.MaxInt32 {
			destination.VT = vtI4
			destination.Value = uint64(uint32(int32(typed)))
		} else {
			destination.VT = vtI8
			destination.Value = uint64(typed)
		}
	case int8:
		destination.VT = vtI2
		destination.Value = uint64(uint16(int16(typed)))
	case int16:
		destination.VT = vtI2
		destination.Value = uint64(uint16(typed))
	case int32:
		destination.VT = vtI4
		destination.Value = uint64(uint32(typed))
	case int64:
		destination.VT = vtI8
		destination.Value = uint64(typed)
	case uint:
		destination.VT = vtUI8
		destination.Value = uint64(typed)
	case uint8:
		destination.VT = vtI4
		destination.Value = uint64(typed)
	case uint16:
		destination.VT = vtI4
		destination.Value = uint64(typed)
	case uint32:
		destination.VT = vtUI8
		destination.Value = uint64(typed)
	case uint64:
		destination.VT = vtUI8
		destination.Value = typed
	case float32:
		destination.VT = vtR4
		destination.Value = uint64(math.Float32bits(typed))
	case float64:
		destination.VT = vtR8
		destination.Value = math.Float64bits(typed)
	case fmt.Stringer:
		return setVariantString(destination, typed.String())
	default:
		data, err := json.Marshal(value)
		if err != nil {
			return eFail
		}
		return setVariantString(destination, string(data))
	}
	return sOK
}

func setVariantString(destination *variant, value string) uint32 {
	bstr, err := allocateBSTR(value)
	if err != nil {
		return eOutOfMemory
	}
	destination.VT = vtBSTR
	destination.Value = uint64(bstr)
	return sOK
}

func variantAsString(source *variant) (string, error) {
	if source == nil {
		return "", errTypeMismatch
	}
	if source.VT == vtBSTR {
		return readBSTR(uintptr(source.Value))
	}

	var converted variant
	result, _, _ := procVariantChangeType.Call(
		uintptr(unsafe.Pointer(&converted)),
		uintptr(unsafe.Pointer(source)),
		0,
		vtBSTR,
	)
	if hresultFailed(result) {
		return "", errTypeMismatch
	}
	defer procVariantClear.Call(uintptr(unsafe.Pointer(&converted)))
	if converted.VT != vtBSTR {
		return "", errTypeMismatch
	}
	return readBSTR(uintptr(converted.Value))
}

func allocateBSTR(value string) (uintptr, error) {
	units := utf16.Encode([]rune(value))
	var pointer uintptr
	if len(units) > 0 {
		pointer = uintptr(unsafe.Pointer(&units[0]))
	}
	result, _, _ := procSysAllocStringLen.Call(pointer, uintptr(len(units)))
	runtime.KeepAlive(units)
	if result == 0 && len(units) > 0 {
		return 0, errors.New("SysAllocStringLen returned NULL")
	}
	return result, nil
}

func readBSTR(pointer uintptr) (string, error) {
	if pointer == 0 {
		return "", nil
	}
	length, _, _ := procSysStringLen.Call(pointer)
	if length > maxBSTRCodeUnits {
		return "", errTypeMismatch
	}
	if length == 0 {
		return "", nil
	}
	required, _, callErr := procWideCharToMultiByte.Call(
		codePageUTF8,
		0,
		pointer,
		length,
		0,
		0,
		0,
		0,
	)
	if required == 0 {
		if callErr != nil && callErr != windows.ERROR_SUCCESS {
			return "", fmt.Errorf("WideCharToMultiByte size: %w", callErr)
		}
		return "", errTypeMismatch
	}
	buffer := make([]byte, int(required))
	written, _, callErr := procWideCharToMultiByte.Call(
		codePageUTF8,
		0,
		pointer,
		length,
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(len(buffer)),
		0,
		0,
	)
	runtime.KeepAlive(buffer)
	if written == 0 {
		if callErr != nil && callErr != windows.ERROR_SUCCESS {
			return "", fmt.Errorf("WideCharToMultiByte: %w", callErr)
		}
		return "", errTypeMismatch
	}
	return string(buffer[:written]), nil
}

func fillException(info *exceptionInfo, err error) {
	if info == nil {
		return
	}
	*info = exceptionInfo{ResultCode: -2147467259} // E_FAIL
	source, _ := allocateBSTR("PCControllerDesktop")
	description := "Automation command failed"
	if err != nil {
		description = err.Error()
	}
	runes := []rune(description)
	if len(runes) > 2048 {
		description = string(runes[:2048])
	}
	detail, _ := allocateBSTR(description)
	info.Source = source
	info.Description = detail
}

func loadFactory(identity uintptr) (*factoryState, bool) {
	value, ok := factoryStates.Load(identity)
	if !ok {
		return nil, false
	}
	state, ok := value.(*factoryState)
	return state, ok
}

func loadDispatch(identity uintptr) (*dispatchState, bool) {
	value, ok := dispatchStates.Load(identity)
	if !ok {
		return nil, false
	}
	state, ok := value.(*dispatchState)
	return state, ok
}

func addReference(counter *atomic.Uint32) uint32 {
	for {
		current := counter.Load()
		if current == 0 || current == math.MaxUint32 {
			return 0
		}
		if counter.CompareAndSwap(current, current+1) {
			return current + 1
		}
	}
}

func releaseReference(counter *atomic.Uint32) uint32 {
	for {
		current := counter.Load()
		if current == 0 {
			return 0
		}
		if counter.CompareAndSwap(current, current-1) {
			return current - 1
		}
	}
}

func guidMatches(pointer *windows.GUID, expected windows.GUID) bool {
	if pointer == nil {
		return false
	}
	return *pointer == expected
}

func hresult(code uint32) uintptr {
	return uintptr(code)
}

func hresultFailed(result uintptr) bool {
	return int32(uint32(result)) < 0
}

func freeBSTR(pointer uintptr) {
	if pointer != 0 {
		procSysFreeString.Call(pointer)
	}
}
