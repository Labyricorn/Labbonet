package interchange

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"strings"
)

var pngSignature = []byte("\x89PNG\r\n\x1a\n")

const (
	maxPNGChunk   = 8 << 20
	maxPNGChunks  = 4096
	maxCardJSON   = 4 << 20
	maxPNGTextB64 = 6 << 20
)

// pngText extracts the card payload from an untrusted PNG: only uncompressed tEXt
// chunks named ccv3 (preferred) or chara are read. Compressed zTXt/iTXt chunks are
// ignored so no decompression is performed on card metadata. Every chunk length is
// checked against the remaining data and text-chunk CRCs are verified.
func pngText(data []byte) (payload []byte, keyword string, err error) {
	if !bytes.HasPrefix(data, pngSignature) {
		return nil, "", errors.New("not a PNG file")
	}
	pos := len(pngSignature)
	found := map[string][]byte{}
	for n := 0; ; n++ {
		if n > maxPNGChunks {
			return nil, "", errors.New("PNG has too many chunks")
		}
		if len(data)-pos < 12 {
			return nil, "", errors.New("PNG is truncated")
		}
		length := binary.BigEndian.Uint32(data[pos:])
		if length > maxPNGChunk || int(length) > len(data)-pos-12 {
			return nil, "", errors.New("PNG chunk length is invalid or truncated")
		}
		typ := string(data[pos+4 : pos+8])
		body := data[pos+8 : pos+8+int(length)]
		crc := binary.BigEndian.Uint32(data[pos+8+int(length):])
		if typ == "tEXt" {
			if crc32.ChecksumIEEE(data[pos+4:pos+8+int(length)]) != crc {
				return nil, "", errors.New("PNG text chunk checksum mismatch")
			}
			key, text, ok := bytes.Cut(body, []byte{0})
			if ok {
				k := strings.ToLower(string(key))
				if (k == "ccv3" || k == "chara") && len(text) <= maxPNGTextB64 {
					if _, dup := found[k]; !dup {
						found[k] = text
					}
				}
			}
		}
		pos += 12 + int(length)
		if typ == "IEND" {
			break
		}
	}
	for _, k := range []string{"ccv3", "chara"} {
		if text, ok := found[k]; ok {
			raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(text)))
			if err != nil {
				return nil, "", errors.New("PNG card payload is not valid base64")
			}
			if len(raw) > maxCardJSON {
				return nil, "", errors.New("PNG card payload is too large")
			}
			return raw, k, nil
		}
	}
	return nil, "", errors.New("PNG contains no embedded character card (ccv3 or chara)")
}

// pngWithText splices tEXt chunks in front of IEND in already-valid PNG bytes.
func pngWithText(clean []byte, texts [][2]string) ([]byte, error) {
	if len(clean) < 12 || !bytes.HasPrefix(clean, pngSignature) || !bytes.Equal(clean[len(clean)-12:len(clean)-8], []byte{0, 0, 0, 0}) || string(clean[len(clean)-8:len(clean)-4]) != "IEND" {
		return nil, errors.New("unexpected PNG structure")
	}
	var out bytes.Buffer
	out.Write(clean[:len(clean)-12])
	for _, t := range texts {
		body := append(append([]byte(t[0]), 0), []byte(t[1])...)
		var hdr [8]byte
		binary.BigEndian.PutUint32(hdr[:], uint32(len(body)))
		copy(hdr[4:], "tEXt")
		out.Write(hdr[:])
		out.Write(body)
		sum := crc32.NewIEEE()
		sum.Write(hdr[4:])
		sum.Write(body)
		var c [4]byte
		binary.BigEndian.PutUint32(c[:], sum.Sum32())
		out.Write(c[:])
	}
	out.Write(clean[len(clean)-12:])
	return out.Bytes(), nil
}
