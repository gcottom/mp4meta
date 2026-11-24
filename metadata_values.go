package mp4meta

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	core "github.com/gcottom/audiometa-core"
)

const (
	itunesTrim = "----:com.apple.iTunes:iTunSMPB"
	mp4Text    = 1
	mp4UTF16   = 2
	mp4JPEG    = 13
	mp4PNG     = 14
	mp4Integer = 21
)

type metadataItem struct {
	box boxRange
	key string
}

type metadataCell struct {
	code uint32
	data []byte
}

// OpenMetadata opens the additive format-independent editor for an MP4 file.
// Keep source open and unchanged until the editor has finished saving. Save
// writes a new file to the supplied writer and does not close either resource.
func OpenMetadata(source io.ReadSeeker) (*core.Editor, error) {
	layout, err := inspectMP4(source)
	if err != nil {
		return nil, err
	}
	fields, custom, err := layout.values()
	if err != nil {
		return nil, err
	}
	return core.NewEditor(fields, custom, func(out io.Writer, edits core.Changes) error {
		return layout.save(out, fields, edits)
	}), nil
}

func (l *mp4Layout) indexItems() error {
	return walkBoxes(l.at, l.ilst.payload(), l.ilst.end, func(b boxRange) error {
		if len(l.items) >= 65536 {
			return errors.New("mp4meta: too many iTunes metadata items")
		}
		key := b.kind
		if key == "----" {
			var mean, name string
			if err := walkBoxes(l.at, b.payload(), b.end, func(child boxRange) error {
				if child.kind != "mean" && child.kind != "name" {
					return nil
				}
				n := child.end - child.payload() - 4
				if n <= 0 || n > 4096 {
					return errors.New("mp4meta: invalid freeform key")
				}
				var flags [4]byte
				if _, err := l.at.ReadAt(flags[:], child.payload()); err != nil {
					return err
				}
				if binary.BigEndian.Uint32(flags[:]) != 0 {
					return errors.New("mp4meta: unsupported freeform key flags")
				}
				v := make([]byte, n)
				if _, err := l.at.ReadAt(v, child.payload()+4); err != nil {
					return err
				}
				if !utf8.Valid(v) || bytes.ContainsRune(v, 0) {
					return errors.New("mp4meta: invalid freeform key encoding")
				}
				switch {
				case child.kind == "mean" && mean == "":
					mean = string(v)
				case child.kind == "name" && name == "":
					name = string(v)
				default:
					return errors.New("mp4meta: duplicate freeform key part")
				}
				return nil
			}); err == nil && mean != "" && !strings.Contains(mean, ":") && name != "" {
				key = freeformKey(mean, name)
			}
		}
		l.items = append(l.items, metadataItem{box: b, key: key})
		return nil
	})
}

func (l *mp4Layout) values() (map[core.Field][]core.Value, map[string][]string, error) {
	candidates := make(map[string]bool)
	for _, def := range core.Fields() {
		for _, key := range mappingKeys(def) {
			candidates[key] = true
		}
	}

	cells := make(map[string][]metadataCell)
	custom := make(map[string][]string)
	for _, item := range l.items {
		customPrefix := freeformKey(core.MP4Namespace, "")
		key, isCustom := core.ParseCustomName(strings.TrimPrefix(item.key, customPrefix))
		isCustom = isCustom && strings.HasPrefix(item.key, customPrefix)
		if !isCustom && !candidates[item.key] {
			continue
		}
		values, err := l.readItem(item)
		if err != nil {
			if errors.Is(err, errInvalidMP4Box) {
				// Keep an undecodable item in its original byte range.
				continue
			}
			return nil, nil, err
		}
		if isCustom {
			for _, value := range values {
				if text, ok := readText(value); ok && !strings.ContainsRune(text, 0) {
					custom[key] = append(custom[key], text)
				}
			}
		} else {
			cells[item.key] = append(cells[item.key], values...)
		}
	}

	fields := make(map[core.Field][]core.Value)
	for _, def := range core.Fields() {
		for _, key := range mappingKeys(def) {
			for _, cell := range cells[key] {
				if v, ok := readField(def, key, cell); ok {
					fields[def.ID] = append(fields[def.ID], v)
					if !def.Multiple {
						break
					}
				}
			}
			if len(fields[def.ID]) != 0 {
				break
			}
		}
	}
	if complete := artworkExtension(cells); len(complete) != 0 {
		fields[core.Artwork] = complete
	}
	if err := l.readExtraFields(fields); err != nil {
		return nil, nil, err
	}
	return fields, custom, nil
}

func (l *mp4Layout) readItem(item metadataItem) ([]metadataCell, error) {
	var cells []metadataCell
	err := walkBoxes(l.at, item.box.payload(), item.box.end, func(b boxRange) error {
		if b.kind != "data" {
			return nil
		}
		n := b.end - b.payload() - 8
		if n < 0 || len(cells) >= 65536 {
			return fmt.Errorf("%w: invalid metadata data child", errInvalidMP4Box)
		}
		var head [8]byte
		if _, err := l.at.ReadAt(head[:], b.payload()); err != nil {
			return err
		}
		if head[0] != 0 {
			return nil
		}
		buf := make([]byte, n)
		if _, err := l.at.ReadAt(buf, b.payload()+8); err != nil {
			return err
		}
		cells = append(cells, metadataCell{binary.BigEndian.Uint32(head[:4]), buf})
		return nil
	})
	return cells, err
}

func mappingKeys(def core.Definition) []string {
	if def.ID == core.Chapters {
		return []string{fallbackField(def.ID)}
	}
	rep := def.Mapping(core.MP4)
	if rep.Key == "" {
		return nil
	}
	keys := make([]string, 0, len(rep.Aliases)+2)
	keys = append(keys, wireKey(rep.Key))
	for _, alias := range rep.Aliases {
		keys = append(keys, wireKey(alias))
	}
	if key := fallbackField(def.ID); def.Mutable && key != keys[0] {
		keys = append(keys, key)
	}
	return keys
}

func wireKey(key string) string { return strings.ReplaceAll(key, "\u00a9", "\251") }

func freeformKey(mean, name string) string { return "----:" + mean + ":" + name }

func fallbackField(field core.Field) string {
	return freeformKey(core.MP4Namespace, core.FallbackName(field))
}

func readText(v metadataCell) (string, bool) {
	switch v.code {
	case 0, mp4Text:
		return string(v.data), utf8.Valid(v.data)
	case mp4UTF16:
		if len(v.data)%2 != 0 {
			return "", false
		}
		words := make([]uint16, 0, len(v.data)/2)
		for len(v.data) != 0 {
			words = append(words, binary.BigEndian.Uint16(v.data[:2]))
			v.data = v.data[2:]
		}
		if len(words) != 0 && words[0] == 0xfeff {
			words = words[1:]
		}
		for i := 0; i < len(words); i++ {
			if words[i] >= 0xd800 && words[i] <= 0xdfff {
				if words[i] >= 0xdc00 || i+1 == len(words) || words[i+1] < 0xdc00 || words[i+1] > 0xdfff {
					return "", false
				}
				i++
			}
		}
		return string(utf16.Decode(words)), true
	default:
		return "", false
	}
}

func readField(def core.Definition, key string, cell metadataCell) (core.Value, bool) {
	if key == "covr" {
		mime := coverMIME(cell.code)
		if mime != "" && pictureFormat(mime, cell.data) {
			return core.PictureValue(core.Picture{MIME: mime, Data: cell.data}), true
		}
		return core.Value{}, false
	}
	encoded, ok := fieldText(def, key, cell)
	if !ok {
		return core.Value{}, false
	}
	v, err := core.Decode(def.ID, encoded)
	return v, err == nil
}

func fieldText(def core.Definition, key string, cell metadataCell) (string, bool) {
	if key == itunesTrim {
		text, ok := readText(cell)
		parts := strings.Fields(text)
		if !ok || len(parts) < 3 {
			return "", false
		}
		index := 1
		if def.ID == core.EncoderPadding {
			index++
		}
		value, err := strconv.ParseUint(parts[index], 16, 32)
		return strconv.FormatUint(value, 10), err == nil
	}
	if strings.HasPrefix(key, "----:") {
		text, ok := readText(cell)
		if replayGainDB(def.ID, key) {
			text = strings.TrimSpace(text)
			if len(text) >= 2 && strings.EqualFold(text[len(text)-2:], "dB") {
				text = strings.TrimSpace(text[:len(text)-2])
			}
		}
		return text, ok
	}

	switch key {
	case "trkn", "disk":
		if cell.code != 0 || len(cell.data) < 6 {
			return "", false
		}
		i := 2
		if def.ID == core.TrackTotal || def.ID == core.DiscTotal {
			i = 4
		}
		n := binary.BigEndian.Uint16(cell.data[i : i+2])
		return strconv.FormatUint(uint64(n), 10), n != 0
	case "gnre":
		if len(cell.data) != 2 {
			return "", false
		}
		return legacyGenre(binary.BigEndian.Uint16(cell.data))
	}
	if key == "tmpo" && cell.code == mp4Integer && legacyTempoPadding(cell.data) {
		return strconv.FormatUint(uint64(binary.BigEndian.Uint16(cell.data[:2])), 10), true
	}
	if _, isInteger := integerWidth(key); isInteger {
		n, ok := readInteger(cell)
		if def.Kind == core.KindBool {
			return strconv.FormatBool(n != 0), ok && (n == 0 || n == 1)
		}
		return strconv.FormatInt(n, 10), ok
	}
	return readText(cell)
}

func readInteger(cell metadataCell) (int64, bool) {
	if cell.code != 0 && cell.code != mp4Integer && cell.code != 22 {
		return 0, false
	}
	n := len(cell.data)
	if n != 1 && n != 2 && n != 3 && n != 4 && n != 8 {
		return 0, false
	}
	var x int64
	if cell.code != 22 && cell.data[0]&128 != 0 {
		x = -1
	}
	for _, b := range cell.data {
		x = x<<8 | int64(b)
	}
	return x, cell.code != 22 || x >= 0
}

func integerWidth(key string) (int, bool) {
	switch key {
	case "cpil", "pcst", "pgap", "shwm", "stik", "rtng", "hdvd", "akID":
		return 1, true
	case "tmpo", "\251mvi", "\251mvc", "mvin", "mvct":
		return 2, true
	case "tves", "tvsn", "cnID", "atID", "geID", "sfID", "cmID":
		return 4, true
	case "plID":
		return 8, true
	}
	return 0, false
}

func pictureFormat(mime string, data []byte) bool {
	_, format, err := image.DecodeConfig(bytes.NewReader(data))
	return err == nil && mime == "image/"+format
}

func newItem(key string, cells []metadataCell) ([]byte, error) {
	if len(cells) == 0 {
		return nil, nil
	}
	var sections [][]byte
	if parts := strings.SplitN(key, ":", 3); len(parts) == 3 && parts[0] == "----" {
		sections = append(sections, makeBox("mean", make([]byte, 4), []byte(parts[1])),
			makeBox("name", make([]byte, 4), []byte(parts[2])))
		key = "----"
	}
	if len(key) != 4 {
		return nil, fmt.Errorf("mp4meta: invalid item name %q", key)
	}
	n := 8
	for _, section := range sections {
		n += len(section)
	}
	for _, v := range cells {
		if len(v.data) > maxMetadataBytes-n-16 {
			return nil, errors.New("mp4meta: metadata item exceeds 64 MiB")
		}
		n += 16 + len(v.data)
	}
	buf := make([]byte, 8, n)
	copy(buf[4:], key)
	for _, section := range sections {
		buf = append(buf, section...)
	}
	for _, v := range cells {
		buf = append(buf, encodedUint32(uint32(16+len(v.data)))...)
		buf = append(buf, "data"...)
		buf = append(buf, encodedUint32(v.code)...)
		buf = append(buf, 0, 0, 0, 0)
		buf = append(buf, v.data...)
	}
	binary.BigEndian.PutUint32(buf[:4], uint32(n))
	return buf, nil
}

func writeCells(def core.Definition, key string, values []core.Value) (string, []metadataCell, error) {
	cells := make([]metadataCell, 0, len(values))
	for _, v := range values {
		cell, ok := directCell(def, key, v)
		if !ok {
			key = fallbackField(def.ID)
			cells = cells[:0]
			for _, value := range values {
				text, err := core.Encode(def.ID, value)
				if err != nil {
					return "", nil, err
				}
				cells = append(cells, metadataCell{mp4Text, []byte(text)})
			}
			break
		}
		cells = append(cells, cell)
	}
	return key, cells, nil
}

func directCell(def core.Definition, key string, value core.Value) (metadataCell, bool) {
	if width, ok := integerWidth(key); ok {
		var n int64
		switch def.Kind {
		case core.KindBool:
			on, valid := value.Bool()
			if !valid {
				return metadataCell{}, false
			}
			if on {
				n = 1
			}
		default:
			var valid bool
			n, valid = value.Int()
			if !valid {
				return metadataCell{}, false
			}
		}
		// Taggers conventionally write signed big-endian atoms. Keep the
		// historical minimum width for small values.
		if width == 1 && (n > math.MaxInt8 || n < math.MinInt8) {
			width = 2
		}
		if width == 2 && (n > math.MaxInt16 || n < math.MinInt16) {
			width = 4
		}
		if width == 4 && (n > math.MaxInt32 || n < math.MinInt32) {
			width = 8
		}
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], uint64(n))
		data := b[8-width:]
		if key == "tmpo" && legacyTempoPadding(data) {
			return metadataCell{}, false
		}
		return metadataCell{mp4Integer, data}, true
	}
	text, err := core.Encode(def.ID, value)
	if replayGainDB(def.ID, key) {
		text += " dB"
	}
	return metadataCell{mp4Text, []byte(text)}, err == nil
}

func replayGainDB(field core.Field, key string) bool {
	if !strings.HasPrefix(key, "----:com.apple.iTunes:") {
		return false
	}
	switch field {
	case core.ReplayGainTrackGain, core.ReplayGainAlbumGain, core.ReplayGainReferenceLoudness:
		return true
	}
	return false
}
