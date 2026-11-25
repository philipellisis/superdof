package pinone

import "testing"

func TestHIDReport(t *testing.T) {
	pkt := []byte{0, 201, 1, 2, 3, 4, 5, 6, 7}
	r := HIDReport(pkt)
	if len(r) != HIDReportSize {
		t.Fatalf("len %d", len(r))
	}
	if r[0] != HIDReportID {
		t.Fatalf("report id %d", r[0])
	}
	for i, b := range pkt {
		if r[1+i] != b {
			t.Fatalf("byte %d: %d != %d", i, r[1+i], b)
		}
	}
	for _, b := range r[1+len(pkt):] {
		if b != 0 {
			t.Fatal("padding must be zero")
		}
	}
}

func TestOpenUnknownSerialFails(t *testing.T) {
	if _, err := Open("definitely-not-a-port"); err == nil {
		t.Fatal("expected error")
	}
}

func TestIsPinOneProduct(t *testing.T) {
	for _, pid := range []uint16{0x9200, 0x9208, 0x9209, 0x92FF} {
		if !IsPinOneProduct(pid) {
			t.Errorf("%#04x should be a PinOne product", pid)
		}
	}
	for _, pid := range []uint16{0x9100, 0x9300, 0x0208, 0x91FF} {
		if IsPinOneProduct(pid) {
			t.Errorf("%#04x should not be a PinOne product", pid)
		}
	}
}

func TestUeventIsPinOne(t *testing.T) {
	cases := map[string]bool{
		"DRIVER=hid-generic\nHID_ID=0003:000039F5:00009208\nHID_NAME=PinOne": true,
		"HID_ID=0003:000039f5:00009209":                                      true, // XInput mode, lower case hex
		"HID_ID=0003:000039F5:000092AB":                                      true, // future firmware mode
		"HID_ID=0003:000039F5:00009300":                                      false,
		"HID_ID=0003:00000E8F:00009208":                                      false, // wrong vendor
		"HID_ID=0005:000039F5:00009208":                                      false, // bluetooth bus
		"HID_ID=0003:000039F5:00019208":                                      false, // PID out of 16 bit range
		"HID_NAME=PinOne":                                                    false,
		"HID_ID=garbage":                                                     false,
	}
	for in, want := range cases {
		if got := ueventIsPinOne(in); got != want {
			t.Errorf("ueventIsPinOne(%q) = %v, want %v", in, got, want)
		}
	}
}
