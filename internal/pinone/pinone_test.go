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
