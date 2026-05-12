package mqttsub

import "testing"

func TestDecode_Number(t *testing.T) {
	v, err := decodeValue([]byte("21.5"), "")
	if err != nil || v != "21.5" {
		t.Errorf("got %q, %v", v, err)
	}
}

func TestDecode_JSONValueKey(t *testing.T) {
	v, err := decodeValue([]byte(`{"value": 42.7}`), "")
	if err != nil || v != "42.7" {
		t.Errorf("got %q, %v", v, err)
	}
}

func TestDecode_PlainString(t *testing.T) {
	v, err := decodeValue([]byte("OK"), "")
	if err != nil || v != "OK" {
		t.Errorf("got %q, %v", v, err)
	}
}

func TestDecode_FieldTopLevel(t *testing.T) {
	payload := []byte(`{"temperature": 26.20188, "humidity": 40.84514}`)
	v, err := decodeValue(payload, "temperature")
	if err != nil || v != "26.20188" {
		t.Errorf("got %q, %v", v, err)
	}
}

func TestDecode_FieldNested(t *testing.T) {
	payload := []byte(`{"particles": {"p25um": 6, "p10um": 24}}`)
	v, err := decodeValue(payload, "particles.p25um")
	if err != nil || v != "6" {
		t.Errorf("got %q, %v", v, err)
	}
}

func TestDecode_FieldMissing(t *testing.T) {
	payload := []byte(`{"temperature": 26}`)
	if _, err := decodeValue(payload, "humidity"); err == nil {
		t.Error("expected error for missing field")
	}
}

func TestDecode_FieldNotJSON(t *testing.T) {
	if _, err := decodeValue([]byte("21.5"), "temperature"); err == nil {
		t.Error("expected error when field requested but payload not JSON object")
	}
}
