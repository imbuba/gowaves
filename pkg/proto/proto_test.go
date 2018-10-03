package proto

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"log"
	"testing"
)

type headerMarshallingTestData struct {
	header        Header
	encodedHeader string
}

var headerMarshallingTests = []headerMarshallingTestData{
	{
		Header{0x42, 0x42000000, 8, 0x666, 0x999},
		"0000004242000000080000066600000999",
	},
	{
		Header{0x4200, 0x420000, 255, 0xaabbddee, 0xdeadbeef},
		"0000420000420000ffaabbddeedeadbeef",
	},
}

func TestHeaderMarshalling(t *testing.T) {
	for _, v := range headerMarshallingTests {
		decoded, err := hex.DecodeString(v.encodedHeader)
		if err != nil {
			log.Fatal(err)
		}

		data, err := v.header.MarshalBinary()
		if err != nil {
			log.Fatal(err)
		}

		res := bytes.Compare(data, decoded)
		if res != 0 {
			strEncoded := hex.EncodeToString(data)
			log.Fatal(fmt.Errorf("want: %s, have: %s", v.encodedHeader, strEncoded))
		}

	}
}

type handshakeMarshallingTestData struct {
	handshake        Handshake
	encodedHandshake string
}

var handshakeMarshallingTests = []handshakeMarshallingTestData{
	{
		Handshake{0x2, "ab", 0x10, 0x3, 0x8, 0x2, "dc", 0x701, 0x2, []byte{10, 20}, 0x8000},
		"0261620000001000000003000000080264630000000000000701000000020a140000000000008000",
	},
	{
		Handshake{0x6, "wavesT", 0x0, 0xe, 0x5, 0xf, "My TESTNET node", 0x1c61, 0x08, []byte{0xb9, 0x29, 0x70, 0x1e, 0x00, 0x00, 0x1a, 0xcf}, 0x5bb482c9},
		"06776176657354000000000000000e000000050f4d7920544553544e4554206e6f64650000000000001c6100000008b929701e00001acf000000005bb482c9",
	},
}

func TestHandshakeMarshalling(t *testing.T) {
	for _, v := range handshakeMarshallingTests {
		decoded, err := hex.DecodeString(v.encodedHandshake)
		if err != nil {
			log.Fatal(err)
		}

		data, err := v.handshake.MarshalBinary()
		if err != nil {
			log.Fatal(err)
		}

		res := bytes.Compare(data, decoded)
		if res != 0 {
			strEncoded := hex.EncodeToString(data)
			log.Fatal(fmt.Errorf("want: %s, have: %s", v.encodedHandshake, strEncoded))
		}
	}
}
