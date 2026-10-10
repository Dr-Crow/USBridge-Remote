// SPDX-License-Identifier: GPL-3.0-only
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"os"
)

const maxVideoBytes = 64 << 10
const maxAudioBytes = 1 << 20

type assetPins struct{ blue, orange, silence string }
type assets struct {
	blue, orange []byte
	pages        [][]byte
}

func loadAssets(dir string, pins assetPins) (assets, error) {
	var a assets
	root, err := os.OpenRoot(dir)
	if err != nil {
		return a, errors.New("fixture assets unavailable")
	}
	defer root.Close()
	a.blue, err = loadAsset(root, blueName, pins.blue, maxVideoBytes)
	if err != nil {
		return assets{}, err
	}
	a.orange, err = loadAsset(root, orangeName, pins.orange, maxVideoBytes)
	if err != nil {
		return assets{}, err
	}
	audio, err := loadAsset(root, silenceName, pins.silence, maxAudioBytes)
	if err != nil {
		return assets{}, err
	}
	if validateFrame(a.blue) != nil || validateFrame(a.orange) != nil || bytes.Equal(a.blue, a.orange) {
		return assets{}, errors.New("fixture video invalid")
	}
	a.pages, err = parseOgg(audio)
	if err != nil {
		return assets{}, err
	}
	return a, nil
}

func loadAsset(root *os.Root, name, pin string, limit int64) ([]byte, error) {
	bad := errors.New("fixture asset invalid")
	want, err := hex.DecodeString(pin)
	if err != nil || len(pin) != 64 || len(want) != sha256.Size {
		return nil, bad
	}
	before, err := root.Lstat(name)
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > limit {
		return nil, bad
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, bad
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return nil, bad
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(b)) != before.Size() || int64(len(b)) > limit {
		return nil, bad
	}
	after, err := root.Lstat(name)
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(opened, after) || after.Size() != before.Size() {
		return nil, bad
	}
	got := sha256.Sum256(b)
	if !bytes.Equal(got[:], want) {
		return nil, bad
	}
	return b, nil
}

// Each asset must be exactly one Annex-B access unit in AUD/SPS/PPS/[SEI]/IDR
// order. Build-time strict full-stream decoding establishes dimensions/pixels;
// SHA256 pins bind these bytes to that verification, rather than trusting SPS.
func validateFrame(b []byte) error {
	bad := errors.New("fixture video invalid")
	var types []byte
	for len(b) > 0 {
		prefix := startCode(b)
		if prefix == 0 {
			return bad
		}
		b = b[prefix:]
		if len(b) == 0 || b[0]&0x80 != 0 {
			return bad
		}
		typ := b[0] & 31
		types = append(types, typ)
		if len(types) > 5 {
			return bad
		}
		end := len(b)
		for i := 1; i < len(b); i++ {
			if startCode(b[i:]) != 0 {
				end = i
				break
			}
		}
		if end < 2 {
			return bad
		}
		b = b[end:]
	}
	if bytes.Equal(types, []byte{9, 7, 8, 5}) || bytes.Equal(types, []byte{9, 7, 8, 6, 5}) {
		return nil
	}
	return bad
}
func startCode(b []byte) int {
	if len(b) >= 4 && b[0] == 0 && b[1] == 0 && b[2] == 0 && b[3] == 1 {
		return 4
	}
	if len(b) >= 3 && b[0] == 0 && b[1] == 0 && b[2] == 1 {
		return 3
	}
	return 0
}

// Only one independently complete packet per page, 5 ms CBR stereo Opus, and a
// single bounded stream are accepted. No Ogg continuation or chained streams.
func parseOgg(b []byte) ([][]byte, error) {
	bad := errors.New("fixture audio invalid")
	var pages [][]byte
	var serial uint32
	for len(b) != 0 {
		if len(pages) >= audioPageCount+2 || len(b) < 28 || string(b[:4]) != "OggS" || b[4] != 0 || b[26] != 1 {
			return nil, bad
		}
		size := 28 + int(b[27])
		if size > len(b) || b[27] == 255 {
			return nil, bad
		}
		page := b[:size]
		seq := len(pages)
		if binary.LittleEndian.Uint32(page[18:22]) != uint32(seq) {
			return nil, bad
		}
		thisSerial := binary.LittleEndian.Uint32(page[14:18])
		if seq == 0 {
			serial = thisSerial
		} else if thisSerial != serial {
			return nil, bad
		}
		if oggChecksum(page) != binary.LittleEndian.Uint32(page[22:26]) {
			return nil, bad
		}
		payload := page[28:]
		granule := binary.LittleEndian.Uint64(page[6:14])
		switch seq {
		case 0:
			if page[5] != 2 || granule != 0 || !bytes.Equal(payload, []byte{'O', 'p', 'u', 's', 'H', 'e', 'a', 'd', 1, 2, 120, 0, 128, 187, 0, 0, 0, 0, 0}) {
				return nil, bad
			}
		case 1:
			if page[5] != 0 || granule != 0 || len(payload) < 16 || string(payload[:8]) != "OpusTags" {
				return nil, bad
			}
		default:
			flags := byte(0)
			expectedGranule := uint64(seq-1) * 240
			if seq == audioPageCount+1 {
				flags = 4
				expectedGranule = pcmSamples + 120
			}
			if page[5] != flags || granule != expectedGranule || len(payload) != 80 || payload[0] != 0xec {
				return nil, bad
			}
		}
		pages = append(pages, page)
		b = b[size:]
	}
	if len(pages) != audioPageCount+2 {
		return nil, bad
	}
	return pages, nil
}

func oggChecksum(b []byte) uint32 {
	var crc uint32
	for i, value := range b {
		if i >= 22 && i < 26 {
			value = 0
		}
		crc ^= uint32(value) << 24
		for bit := 0; bit < 8; bit++ {
			if crc&0x80000000 != 0 {
				crc = crc<<1 ^ 0x04c11db7
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}
