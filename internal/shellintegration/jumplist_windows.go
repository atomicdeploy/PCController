//go:build windows

package shellintegration

import (
	"fmt"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	coinitApartmentThreaded = 0x2
	clsctxInprocServer      = 0x1

	hresultFileNotFound = 0x80070002
	hresultPathNotFound = 0x80070003
	hresultAccessDenied = 0x80070005
	rpcEChangedMode     = 0x80010106
)

var (
	jumpListOle32   = windows.NewLazySystemDLL("ole32.dll")
	jumpListPropsys = windows.NewLazySystemDLL("propsys.dll")

	jumpListCoInitializeEx = jumpListOle32.NewProc("CoInitializeEx")
	jumpListCoUninitialize = jumpListOle32.NewProc("CoUninitialize")
	jumpListCoCreate       = jumpListOle32.NewProc("CoCreateInstance")
	jumpListPropClear      = jumpListOle32.NewProc("PropVariantClear")
	jumpListInitPropString = jumpListPropsys.NewProc("InitPropVariantFromString")

	installJumpList = installJumpListWindows
	deleteJumpList  = deleteJumpListWindows
)

var (
	clsidDestinationList = windows.GUID{
		Data1: 0x77F10CF0,
		Data2: 0x3DB5,
		Data3: 0x4966,
		Data4: [8]byte{0xB5, 0x20, 0xB7, 0xC5, 0x4F, 0xD3, 0x5E, 0xD6},
	}
	iidCustomDestinationList = windows.GUID{
		Data1: 0x6332DEBF,
		Data2: 0x87B5,
		Data3: 0x4670,
		Data4: [8]byte{0x90, 0xC0, 0x5E, 0x57, 0xB4, 0x08, 0xA4, 0x9E},
	}
	clsidEnumerableObjectCollection = windows.GUID{
		Data1: 0x2D3468C1,
		Data2: 0x36A7,
		Data3: 0x43B6,
		Data4: [8]byte{0xAC, 0x24, 0xD3, 0xF0, 0x2F, 0xD9, 0x60, 0x7A},
	}
	iidObjectCollection = windows.GUID{
		Data1: 0x5632B1A4,
		Data2: 0xE38A,
		Data3: 0x400A,
		Data4: [8]byte{0x92, 0x8A, 0xD4, 0xCD, 0x63, 0x23, 0x02, 0x95},
	}
	iidObjectArray = windows.GUID{
		Data1: 0x92CA9DCD,
		Data2: 0x5622,
		Data3: 0x4BBA,
		Data4: [8]byte{0xA8, 0x05, 0x5E, 0x9F, 0x54, 0x1B, 0xD8, 0xC9},
	}
	clsidShellLink = windows.GUID{
		Data1: 0x00021401,
		Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46},
	}
	iidShellLinkW = windows.GUID{
		Data1: 0x000214F9,
		Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46},
	}
	iidPropertyStore = windows.GUID{
		Data1: 0x886D8EEB,
		Data2: 0x8CF2,
		Data3: 0x4446,
		Data4: [8]byte{0x8D, 0x02, 0xCD, 0xBA, 0x1D, 0xBD, 0xCF, 0x99},
	}
	pkeyTitle = propertyKey{
		FormatID: windows.GUID{
			Data1: 0xF29F85E0,
			Data2: 0x4FF9,
			Data3: 0x1068,
			Data4: [8]byte{0xAB, 0x91, 0x08, 0x00, 0x2B, 0x27, 0xB3, 0xD9},
		},
		PropertyID: 2,
	}
)

type customDestinationList struct {
	VTable *customDestinationListVTable
}

type customDestinationListVTable struct {
	QueryInterface         uintptr
	AddRef                 uintptr
	Release                uintptr
	SetAppID               uintptr
	BeginList              uintptr
	AppendCategory         uintptr
	AppendKnownCategory    uintptr
	AddUserTasks           uintptr
	CommitList             uintptr
	GetRemovedDestinations uintptr
	DeleteList             uintptr
	AbortList              uintptr
}

type objectCollection struct {
	VTable *objectCollectionVTable
}

type objectCollectionVTable struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
	GetCount       uintptr
	GetAt          uintptr
	AddObject      uintptr
	AddFromArray   uintptr
	RemoveObjectAt uintptr
	Clear          uintptr
}

type objectArray struct {
	VTable *objectArrayVTable
}

type objectArrayVTable struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
	GetCount       uintptr
	GetAt          uintptr
}

type shellLink struct {
	VTable *shellLinkVTable
}

type shellLinkVTable struct {
	QueryInterface      uintptr
	AddRef              uintptr
	Release             uintptr
	GetPath             uintptr
	GetIDList           uintptr
	SetIDList           uintptr
	GetDescription      uintptr
	SetDescription      uintptr
	GetWorkingDirectory uintptr
	SetWorkingDirectory uintptr
	GetArguments        uintptr
	SetArguments        uintptr
	GetHotkey           uintptr
	SetHotkey           uintptr
	GetShowCmd          uintptr
	SetShowCmd          uintptr
	GetIconLocation     uintptr
	SetIconLocation     uintptr
	SetRelativePath     uintptr
	Resolve             uintptr
	SetPath             uintptr
}

type propertyStore struct {
	VTable *propertyStoreVTable
}

type propertyStoreVTable struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
	GetCount       uintptr
	GetAt          uintptr
	GetValue       uintptr
	SetValue       uintptr
	Commit         uintptr
}

type propertyKey struct {
	FormatID   windows.GUID
	PropertyID uint32
}

// propVariant has the native PROPVARIANT size and pointer alignment on both
// 32-bit and 64-bit Windows. InitPropVariantFromString owns its payload until
// PropVariantClear is called.
type propVariant struct {
	Type      uint16
	Reserved1 uint16
	Reserved2 uint16
	Reserved3 uint16
	Data      [2]uintptr
}

func installJumpListWindows(executable string) error {
	absolute, err := validatedExecutable(executable)
	if err != nil {
		return err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	uninitialize, err := initializeJumpListCOM()
	if err != nil {
		return err
	}
	defer uninitialize()

	list, err := createDestinationList()
	if err != nil {
		return err
	}
	defer list.release()

	appID, err := windows.UTF16PtrFromString(AppUserModelID)
	if err != nil {
		return fmt.Errorf("shellintegration: encode AppUserModelID: %w", err)
	}
	result, _, _ := syscall.SyscallN(
		list.VTable.SetAppID,
		uintptr(unsafe.Pointer(list)),
		uintptr(unsafe.Pointer(appID)),
	)
	runtime.KeepAlive(appID)
	if err := jumpHRESULT("ICustomDestinationList.SetAppID", result); err != nil {
		return err
	}

	var minimumSlots uint32
	var removed *objectArray
	result, _, _ = syscall.SyscallN(
		list.VTable.BeginList,
		uintptr(unsafe.Pointer(list)),
		uintptr(unsafe.Pointer(&minimumSlots)),
		uintptr(unsafe.Pointer(&iidObjectArray)),
		uintptr(unsafe.Pointer(&removed)),
	)
	if err := jumpHRESULT("ICustomDestinationList.BeginList", result); err != nil {
		return err
	}
	if removed != nil {
		removed.release()
	}
	committed := false
	defer func() {
		if !committed {
			list.abort()
		}
	}()

	collection, err := createObjectCollection()
	if err != nil {
		return err
	}
	defer collection.release()

	for _, task := range jumpTasks {
		link, err := createTaskShellLink(absolute, task)
		if err != nil {
			return err
		}
		result, _, _ = syscall.SyscallN(
			collection.VTable.AddObject,
			uintptr(unsafe.Pointer(collection)),
			uintptr(unsafe.Pointer(link)),
		)
		link.release()
		if err := jumpHRESULT("IObjectCollection.AddObject", result); err != nil {
			return err
		}
	}

	array, err := collection.asObjectArray()
	if err != nil {
		return err
	}
	defer array.release()
	result, _, _ = syscall.SyscallN(
		list.VTable.AddUserTasks,
		uintptr(unsafe.Pointer(list)),
		uintptr(unsafe.Pointer(array)),
	)
	if err := jumpHRESULT("ICustomDestinationList.AddUserTasks", result); err != nil {
		return err
	}
	result, _, _ = syscall.SyscallN(
		list.VTable.CommitList,
		uintptr(unsafe.Pointer(list)),
	)
	if err := jumpHRESULT("ICustomDestinationList.CommitList", result); err != nil {
		return err
	}
	committed = true
	return nil
}

func deleteJumpListWindows() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	uninitialize, err := initializeJumpListCOM()
	if err != nil {
		return err
	}
	defer uninitialize()
	list, err := createDestinationList()
	if err != nil {
		return err
	}
	defer list.release()
	appID, err := windows.UTF16PtrFromString(AppUserModelID)
	if err != nil {
		return fmt.Errorf("shellintegration: encode AppUserModelID: %w", err)
	}
	result, _, _ := syscall.SyscallN(
		list.VTable.DeleteList,
		uintptr(unsafe.Pointer(list)),
		uintptr(unsafe.Pointer(appID)),
	)
	runtime.KeepAlive(appID)
	code := uint32(result)
	if code == hresultFileNotFound || code == hresultPathNotFound {
		return nil
	}
	return jumpHRESULT("ICustomDestinationList.DeleteList", result)
}

func initializeJumpListCOM() (func(), error) {
	result, _, _ := jumpListCoInitializeEx.Call(0, coinitApartmentThreaded)
	if uint32(result) == rpcEChangedMode {
		// COM is already initialized on this locked thread under another
		// apartment model. CoCreateInstance remains available; because this
		// call did not initialize COM, it must not balance with CoUninitialize.
		return func() {}, nil
	}
	if err := jumpHRESULT("CoInitializeEx", result); err != nil {
		return nil, err
	}
	return func() {
		jumpListCoUninitialize.Call()
	}, nil
}

func createDestinationList() (*customDestinationList, error) {
	var list *customDestinationList
	if err := coCreate(
		&clsidDestinationList,
		&iidCustomDestinationList,
		unsafe.Pointer(&list),
	); err != nil {
		return nil, fmt.Errorf("shellintegration: create destination list: %w", err)
	}
	return list, nil
}

func createObjectCollection() (*objectCollection, error) {
	var collection *objectCollection
	if err := coCreate(
		&clsidEnumerableObjectCollection,
		&iidObjectCollection,
		unsafe.Pointer(&collection),
	); err != nil {
		return nil, fmt.Errorf("shellintegration: create task collection: %w", err)
	}
	return collection, nil
}

func createTaskShellLink(
	executable string,
	task jumpTask,
) (*shellLink, error) {
	var link *shellLink
	if err := coCreate(
		&clsidShellLink,
		&iidShellLinkW,
		unsafe.Pointer(&link),
	); err != nil {
		return nil, fmt.Errorf("shellintegration: create task %q: %w", task.title, err)
	}
	success := false
	defer func() {
		if !success {
			link.release()
		}
	}()

	if err := link.setString(link.VTable.SetPath, executable, "IShellLink.SetPath"); err != nil {
		return nil, err
	}
	if err := link.setString(
		link.VTable.SetArguments,
		task.arguments,
		"IShellLink.SetArguments",
	); err != nil {
		return nil, err
	}
	if err := link.setString(
		link.VTable.SetWorkingDirectory,
		filepath.Dir(executable),
		"IShellLink.SetWorkingDirectory",
	); err != nil {
		return nil, err
	}
	if err := link.setString(
		link.VTable.SetDescription,
		task.description,
		"IShellLink.SetDescription",
	); err != nil {
		return nil, err
	}
	iconPath, err := windows.UTF16PtrFromString(executable)
	if err != nil {
		return nil, fmt.Errorf("shellintegration: encode task icon path: %w", err)
	}
	result, _, _ := syscall.SyscallN(
		link.VTable.SetIconLocation,
		uintptr(unsafe.Pointer(link)),
		uintptr(unsafe.Pointer(iconPath)),
		0,
	)
	runtime.KeepAlive(iconPath)
	if err := jumpHRESULT("IShellLink.SetIconLocation", result); err != nil {
		return nil, err
	}
	if err := link.setTitle(task.title); err != nil {
		return nil, err
	}
	success = true
	return link, nil
}

func (link *shellLink) setString(
	method uintptr,
	value,
	operation string,
) error {
	pointer, err := windows.UTF16PtrFromString(value)
	if err != nil {
		return fmt.Errorf("shellintegration: encode %s: %w", operation, err)
	}
	result, _, _ := syscall.SyscallN(
		method,
		uintptr(unsafe.Pointer(link)),
		uintptr(unsafe.Pointer(pointer)),
	)
	runtime.KeepAlive(pointer)
	return jumpHRESULT(operation, result)
}

func (link *shellLink) setTitle(title string) error {
	var store *propertyStore
	result, _, _ := syscall.SyscallN(
		link.VTable.QueryInterface,
		uintptr(unsafe.Pointer(link)),
		uintptr(unsafe.Pointer(&iidPropertyStore)),
		uintptr(unsafe.Pointer(&store)),
	)
	if err := jumpHRESULT("IShellLink.QueryInterface(IPropertyStore)", result); err != nil {
		return err
	}
	defer store.release()

	titlePointer, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return fmt.Errorf("shellintegration: encode task title: %w", err)
	}
	var value propVariant
	result, _, _ = jumpListInitPropString.Call(
		uintptr(unsafe.Pointer(titlePointer)),
		uintptr(unsafe.Pointer(&value)),
	)
	runtime.KeepAlive(titlePointer)
	if err := jumpHRESULT("InitPropVariantFromString", result); err != nil {
		return err
	}
	defer jumpListPropClear.Call(uintptr(unsafe.Pointer(&value)))

	result, _, _ = syscall.SyscallN(
		store.VTable.SetValue,
		uintptr(unsafe.Pointer(store)),
		uintptr(unsafe.Pointer(&pkeyTitle)),
		uintptr(unsafe.Pointer(&value)),
	)
	if err := jumpHRESULT("IPropertyStore.SetValue(PKEY_Title)", result); err != nil {
		return err
	}
	result, _, _ = syscall.SyscallN(
		store.VTable.Commit,
		uintptr(unsafe.Pointer(store)),
	)
	return jumpHRESULT("IPropertyStore.Commit", result)
}

func coCreate(
	classID,
	interfaceID *windows.GUID,
	result unsafe.Pointer,
) error {
	code, _, _ := jumpListCoCreate.Call(
		uintptr(unsafe.Pointer(classID)),
		0,
		clsctxInprocServer,
		uintptr(unsafe.Pointer(interfaceID)),
		uintptr(result),
	)
	return jumpHRESULT("CoCreateInstance", code)
}

func (collection *objectCollection) asObjectArray() (*objectArray, error) {
	var array *objectArray
	result, _, _ := syscall.SyscallN(
		collection.VTable.QueryInterface,
		uintptr(unsafe.Pointer(collection)),
		uintptr(unsafe.Pointer(&iidObjectArray)),
		uintptr(unsafe.Pointer(&array)),
	)
	if err := jumpHRESULT("IObjectCollection.QueryInterface(IObjectArray)", result); err != nil {
		return nil, err
	}
	return array, nil
}

func (list *customDestinationList) abort() {
	if list == nil || list.VTable == nil {
		return
	}
	syscall.SyscallN(list.VTable.AbortList, uintptr(unsafe.Pointer(list)))
}

func (list *customDestinationList) release() {
	if list == nil || list.VTable == nil {
		return
	}
	syscall.SyscallN(list.VTable.Release, uintptr(unsafe.Pointer(list)))
}

func (collection *objectCollection) release() {
	if collection == nil || collection.VTable == nil {
		return
	}
	syscall.SyscallN(
		collection.VTable.Release,
		uintptr(unsafe.Pointer(collection)),
	)
}

func (array *objectArray) release() {
	if array == nil || array.VTable == nil {
		return
	}
	syscall.SyscallN(array.VTable.Release, uintptr(unsafe.Pointer(array)))
}

func (link *shellLink) release() {
	if link == nil || link.VTable == nil {
		return
	}
	syscall.SyscallN(link.VTable.Release, uintptr(unsafe.Pointer(link)))
}

func (store *propertyStore) release() {
	if store == nil || store.VTable == nil {
		return
	}
	syscall.SyscallN(store.VTable.Release, uintptr(unsafe.Pointer(store)))
}

type jumpListHRESULT struct {
	operation string
	code      uint32
}

func (e jumpListHRESULT) Error() string {
	if e.code == hresultAccessDenied {
		return fmt.Sprintf(
			"shellintegration: %s was denied; Windows Jump Lists may be disabled by policy",
			e.operation,
		)
	}
	return fmt.Sprintf(
		"shellintegration: %s failed with HRESULT 0x%08X",
		e.operation,
		e.code,
	)
}

func jumpHRESULT(operation string, result uintptr) error {
	code := uint32(result)
	if int32(code) >= 0 {
		return nil
	}
	return jumpListHRESULT{operation: operation, code: code}
}
