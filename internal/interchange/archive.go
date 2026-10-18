package interchange

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/Labyricorn/Labbonet/internal/cast"
)

const (
	maxArchiveEntries = 64
	maxEntryBytes     = 8 << 20
	maxArchiveTotal   = 24 << 20
)

var executableExt = map[string]bool{
	".exe": true, ".dll": true, ".bat": true, ".cmd": true, ".com": true, ".scr": true, ".msi": true,
	".ps1": true, ".psm1": true, ".vbs": true, ".js": true, ".mjs": true, ".jar": true, ".sh": true,
	".py": true, ".lnk": true, ".hta": true, ".wasm": true,
}

// safeName rejects traversal, absolute, drive-qualified and control-character names.
func safeName(name string) bool {
	if name == "" || len(name) > 240 || strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	for _, seg := range strings.Split(strings.TrimSuffix(name, "/"), "/") {
		if seg == ".." || seg == "." || seg == "" {
			return false
		}
	}
	return true
}

// readArchive validates every entry before any content is trusted, then reads
// entries with hard limits that do not depend on the declared sizes.
func readArchive(data []byte) (map[string][]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("archive is not a valid ZIP file")
	}
	if len(zr.File) > maxArchiveEntries {
		return nil, fmt.Errorf("archive has more than %d entries", maxArchiveEntries)
	}
	files := map[string][]byte{}
	var total int64
	for _, f := range zr.File {
		if !safeName(f.Name) {
			return nil, errors.New("archive contains an unsafe path")
		}
		if f.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("archive contains a symbolic link")
		}
		if f.FileInfo().IsDir() {
			continue
		}
		if executableExt[strings.ToLower(path.Ext(f.Name))] {
			return nil, errors.New("archive contains executable content and was rejected")
		}
		key := strings.ToLower(f.Name)
		if _, dup := files[key]; dup {
			return nil, errors.New("archive contains duplicate entries")
		}
		if f.UncompressedSize64 > maxEntryBytes {
			return nil, errors.New("archive entry is too large")
		}
		if f.UncompressedSize64 > 256<<10 && f.CompressedSize64 > 0 && f.UncompressedSize64/f.CompressedSize64 > 100 {
			return nil, errors.New("archive entry has an excessive compression ratio")
		}
		rc, err := f.Open()
		if err != nil {
			return nil, errors.New("archive entry could not be opened")
		}
		b, err := io.ReadAll(io.LimitReader(rc, maxEntryBytes+1))
		rc.Close()
		if err != nil {
			return nil, errors.New("archive entry is corrupt")
		}
		if len(b) > maxEntryBytes {
			return nil, errors.New("archive entry exceeds its limit")
		}
		total += int64(len(b))
		if total > maxArchiveTotal {
			return nil, errors.New("archive expands beyond the total limit")
		}
		files[key] = b
	}
	return files, nil
}

func parseArchive(data []byte) (*parsedCard, error) {
	files, err := readArchive(data)
	if err != nil {
		return nil, err
	}
	raw, ok := files["card.json"]
	if !ok {
		return nil, errors.New("archive has no card.json")
	}
	p, err := parseCardJSON(raw)
	if err != nil {
		return nil, err
	}
	p.Format += " archive"
	var img []byte
	switch {
	case strings.HasPrefix(strings.ToLower(p.Icon), "embeded://"):
		img = files[strings.ToLower(strings.TrimPrefix(p.Icon[len("embeded://"):], "/"))]
		if img == nil {
			p.Notes = append(p.Notes, "referenced icon asset was not found in the archive")
		}
	case strings.HasPrefix(strings.ToLower(p.Icon), "http"):
		p.Notes = append(p.Notes, "remote icon URL was not fetched")
	}
	if img == nil {
		for _, n := range []string{"portrait.png", "portrait.jpg", "portrait.jpeg", "avatar.png", "avatar.jpg"} {
			if b, ok := files[n]; ok {
				img = b
				break
			}
		}
	}
	if img != nil {
		uri, note, err := normalizeImage(img)
		if err != nil {
			p.Notes = append(p.Notes, "portrait skipped: "+err.Error())
		} else {
			p.Card.Portrait = uri
			if note != "" {
				p.Notes = append(p.Notes, note)
			}
		}
	}
	used := 1
	if img != nil {
		used = 2
	}
	if len(files) > used {
		p.Notes = append(p.Notes, "other archive entries (assets, modules) were ignored")
	}
	return p, nil
}

func parsePNGCard(data []byte) (*parsedCard, error) {
	raw, keyword, err := pngText(data)
	if err != nil {
		return nil, err
	}
	p, err := parseCardJSON(raw)
	if err != nil {
		return nil, err
	}
	p.Format += " PNG (" + keyword + ")"
	uri, note, err := normalizeImage(data)
	if err != nil {
		p.Notes = append(p.Notes, "portrait skipped: "+err.Error())
	} else {
		p.Card.Portrait = uri
		if note != "" {
			p.Notes = append(p.Notes, note)
		}
	}
	return p, nil
}

// ExportPNG writes a Tavern V3 PNG with a V2 backfill chunk for older tools.
func ExportPNG(c cast.Card) ([]byte, error) {
	clean, err := portraitPNG(c.Portrait)
	if err != nil {
		return nil, err
	}
	enc := func(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
	return pngWithText(clean, [][2]string{{"chara", enc(ExportV2JSON(c))}, {"ccv3", enc(ExportV3JSON(c))}})
}

// ExportZIP writes the sharing archive: card.json (V3) plus an embedded icon.
func ExportZIP(c cast.Card) ([]byte, error) {
	icon, err := portraitPNG(c.Portrait)
	if err != nil {
		return nil, err
	}
	v3 := bytes.Replace(ExportV3JSON(c), []byte(`"uri": "ccdefault:"`), []byte(`"uri": "embeded://assets/icon/images/main.png"`), 1)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range []struct {
		name string
		data []byte
	}{{"card.json", v3}, {"assets/icon/images/main.png", icon}} {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: f.name, Method: zip.Deflate})
		if err != nil {
			return nil, err
		}
		if _, err = w.Write(f.data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
