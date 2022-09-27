package api

import (
	"fmt"
	"net"
	"testing"
	"testing/quick"
)

func TestLengthEncoding(t *testing.T) {
	f := func(input uint32) bool {
		encodedLength := encodeLength(input)
		lengthBytes := decodeLengthByte(encodedLength[0])
		if lengthBytes != len(encodedLength) {
			fmt.Printf("failed on number of bytes, %v != %v\n", lengthBytes, len(encodedLength))
			return false
		}
		output := decodeLength(encodedLength)
		if input != output {
			fmt.Printf("Mismatch input and output, %x != %x\n", input, output)
		}
		return input == output
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 1000000}); err != nil {
		t.Error(err)
	}
}

func TestLengthEncode(t *testing.T) {
	tests := []struct {
		expected []byte
		input    uint32
	}{
		{
			[]byte{0x7F},
			uint32(0x7F),
		},
		{
			[]byte{0xBF, 0xFF},
			uint32(0x3FFF),
		},
		{
			[]byte{0xDF, 0xFF, 0xFF},
			uint32(0x1FFFFF),
		},
		{
			[]byte{0xEF, 0xFF, 0xFF, 0xFF},
			uint32(0x0FFFFFFF),
		},
		{
			[]byte{0xF0, 0x10, 0x00, 0x00, 0x00},
			uint32(0x10000000),
		},
		{
			[]byte{0xF0, 0xFF, 0xFF, 0xFF, 0xFF},
			uint32(0xFFFFFFFF),
		},
	}

	for testI, test := range tests {
		expected := test.expected
		encoded := encodeLength(test.input)

		if len(expected) != len(encoded) {
			t.Logf("Test vector %v failed, number of bytes encoded, %v != %v", testI+1, len(expected), len(encoded))
			t.Fail()
		}

		for i, b := range expected {
			if b != encoded[i] {
				t.Logf("Test vector %v failed, encoded does not match expected, %x != %x   %x", testI+1, expected, encoded, test.input)
				t.Fail()
				break
			}
		}

		lengthBytes := decodeLengthByte(encoded[0])
		if lengthBytes != len(encoded) {
			t.Logf("Test vector %v failed, decoded length byets does not match encoded length, %v != %v", testI+1, lengthBytes, len(encoded))
			t.Fail()
		}
		decoded := decodeLength(encoded)
		if test.input != decoded {
			t.Logf("Test vector %v failed, decoded value does not match input, %x != %x", testI+1, test.input, decoded)
			t.Fail()
		}
	}
}

func TestLogin(t *testing.T) {
	client, err := New(
		net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8728},
		"admin",
		"test",
	)
	if err != nil {
		t.Error(err)
	}

	client.Close()
}

func TestLogin_IncorrectPassword(t *testing.T) {
	client, err := New(
		net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8728},
		"admin",
		"wrong",
	)
	if err == nil {
		t.Error("did not fail on wrong password")
	}

	client.Close()
}

func TestBeep(t *testing.T) {
	client, err := New(
		net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8728},
		"admin",
		"test",
	)
	if err != nil {
		t.Error(err)
	}

	err, _ = client.SendCommand("/beep", map[string]string{})
	if err != nil {
		t.Error(err)
	}

	client.Close()
}

func TestInvalidCommand(t *testing.T) {
	client, err := New(
		net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8728},
		"admin",
		"test",
	)
	if err != nil {
		t.Error(err)
	}

	err, _ = client.SendCommand("/wrong", map[string]string{})
	if err == nil {
		t.Error("Did not fail on invalid command")
	}

	client.Close()
}
