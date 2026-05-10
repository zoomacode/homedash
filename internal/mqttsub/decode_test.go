package mqttsub

import "testing"

func TestDecode_Number(t *testing.T) {
	v, err := decodeValue([]byte("21.5"))
	if err != nil || v != "21.5" {
		t.Errorf("got %q, %v", v, err)
	}
}

func TestDecode_JSON(t *testing.T) {
	v, err := decodeValue([]byte(`{"value": 42.7}`))
	if err != nil || v != "42.7" {
		t.Errorf("got %q, %v", v, err)
	}
}

func TestDecode_PlainString(t *testing.T) {
	v, err := decodeValue([]byte("OK"))
	if err != nil || v != "OK" {
		t.Errorf("got %q, %v", v, err)
	}
}
