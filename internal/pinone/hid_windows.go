//go:build windows

package pinone

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	modHid      = syscall.NewLazyDLL("hid.dll")
	modSetupapi = syscall.NewLazyDLL("setupapi.dll")
	modKernel32 = syscall.NewLazyDLL("kernel32.dll")

	procHidGetGuid       = modHid.NewProc("HidD_GetHidGuid")
	procHidGetAttributes = modHid.NewProc("HidD_GetAttributes")
	procHidGetPreparsed  = modHid.NewProc("HidD_GetPreparsedData")
	procHidFreePreparsed = modHid.NewProc("HidD_FreePreparsedData")
	procHidGetCaps       = modHid.NewProc("HidP_GetCaps")

	procSetupGetClassDevs    = modSetupapi.NewProc("SetupDiGetClassDevsW")
	procSetupEnumInterfaces  = modSetupapi.NewProc("SetupDiEnumDeviceInterfaces")
	procSetupGetIfaceDetail  = modSetupapi.NewProc("SetupDiGetDeviceInterfaceDetailW")
	procSetupDestroyInfoList = modSetupapi.NewProc("SetupDiDestroyDeviceInfoList")
)

const (
	digcfPresent         = 0x2
	digcfDeviceInterface = 0x10
)

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

type spDeviceInterfaceData struct {
	CbSize             uint32
	InterfaceClassGuid guid
	Flags              uint32
	Reserved           uintptr
}

type hidAttributes struct {
	Size      uint32
	VendorID  uint16
	ProductID uint16
	Version   uint16
}

type hidpCaps struct {
	Usage                     uint16
	UsagePage                 uint16
	InputReportByteLength     uint16
	OutputReportByteLength    uint16
	FeatureReportByteLength   uint16
	Reserved                  [17]uint16
	NumberLinkCollectionNodes uint16
	NumberInputButtonCaps     uint16
	NumberInputValueCaps      uint16
	NumberInputDataIndices    uint16
	NumberOutputButtonCaps    uint16
	NumberOutputValueCaps     uint16
	NumberOutputDataIndices   uint16
	NumberFeatureButtonCaps   uint16
	NumberFeatureValueCaps    uint16
	NumberFeatureDataIndices  uint16
}

type hidDevice struct {
	h    syscall.Handle
	path string
}

func (d *hidDevice) Describe() string { return "USB HID " + d.path }

func (d *hidDevice) Close() error {
	if d.h != 0 && d.h != syscall.InvalidHandle {
		err := syscall.CloseHandle(d.h)
		d.h = syscall.InvalidHandle
		return err
	}
	return nil
}

func (d *hidDevice) Write(packet []byte) error {
	buf := HIDReport(packet)
	for tries := 0; tries < 3; tries++ {
		var n uint32
		err := syscall.WriteFile(d.h, buf, &n, nil)
		if err == nil {
			if int(n) != len(buf) {
				return fmt.Errorf("short HID write: %d of %d bytes", n, len(buf))
			}
			return nil
		}
		if errno, ok := err.(syscall.Errno); ok && errno == syscall.Errno(6) {
			if h, e := syscall.CreateFile(syscall.StringToUTF16Ptr(d.path), syscall.GENERIC_READ|syscall.GENERIC_WRITE,
				syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0, 0); e == nil {
				d.h = h
				continue
			}
		}
		return fmt.Errorf("USB HID write: %w", err)
	}
	return fmt.Errorf("USB HID write failed")
}

// listHIDPaths returns the device paths of all PinOne vendor channels.
func listHIDPaths() ([]string, error) {
	if err := modHid.Load(); err != nil {
		return nil, err
	}
	var g guid
	procHidGetGuid.Call(uintptr(unsafe.Pointer(&g)))
	set, _, e := procSetupGetClassDevs.Call(uintptr(unsafe.Pointer(&g)), 0, 0, digcfPresent|digcfDeviceInterface)
	if set == 0 || set == ^uintptr(0) {
		return nil, fmt.Errorf("SetupDiGetClassDevs: %v", e)
	}
	defer procSetupDestroyInfoList.Call(set)

	var out []string
	for i := uint32(0); ; i++ {
		var di spDeviceInterfaceData
		di.CbSize = uint32(unsafe.Sizeof(di))
		r, _, _ := procSetupEnumInterfaces.Call(set, 0, uintptr(unsafe.Pointer(&g)), uintptr(i), uintptr(unsafe.Pointer(&di)))
		if r == 0 {
			break
		}
		var need uint32
		procSetupGetIfaceDetail.Call(set, uintptr(unsafe.Pointer(&di)), 0, 0, uintptr(unsafe.Pointer(&need)), 0)
		if need == 0 {
			continue
		}
		buf := make([]byte, need)
		cb := uint32(8)
		if unsafe.Sizeof(uintptr(0)) == 4 {
			cb = 6
		}
		*(*uint32)(unsafe.Pointer(&buf[0])) = cb
		r, _, _ = procSetupGetIfaceDetail.Call(set, uintptr(unsafe.Pointer(&di)), uintptr(unsafe.Pointer(&buf[0])), uintptr(need), uintptr(unsafe.Pointer(&need)), 0)
		if r == 0 {
			continue
		}
		path := syscall.UTF16ToString((*[1 << 15]uint16)(unsafe.Pointer(&buf[4]))[:(len(buf)-4)/2])

		h, err := syscall.CreateFile(syscall.StringToUTF16Ptr(path), syscall.GENERIC_READ|syscall.GENERIC_WRITE,
			syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0, 0)
		if err != nil {
			continue
		}
		var at hidAttributes
		at.Size = uint32(unsafe.Sizeof(at))
		ok, _, _ := procHidGetAttributes.Call(uintptr(h), uintptr(unsafe.Pointer(&at)))
		match := false
		if ok != 0 && at.VendorID == HIDVendorID && at.ProductID == HIDProductID {
			var pp uintptr
			if r, _, _ := procHidGetPreparsed.Call(uintptr(h), uintptr(unsafe.Pointer(&pp))); r != 0 {
				var caps hidpCaps
				procHidGetCaps.Call(pp, uintptr(unsafe.Pointer(&caps)))
				procHidFreePreparsed.Call(pp)
				match = caps.UsagePage == HIDUsagePage && caps.Usage == HIDUsage
			}
		}
		syscall.CloseHandle(h)
		if match {
			out = append(out, path)
		}
	}
	return out, nil
}

func openHID() (Transport, error) {
	paths, err := listHIDPaths()
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, ErrNotFound
	}
	h, err := syscall.CreateFile(syscall.StringToUTF16Ptr(paths[0]), syscall.GENERIC_READ|syscall.GENERIC_WRITE,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("open PinOne HID device: %w", err)
	}
	return &hidDevice{h: h, path: paths[0]}, nil
}
