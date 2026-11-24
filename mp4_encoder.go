package mp4meta

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	mp4lib "github.com/abema/go-mp4"
	"github.com/aler9/writerseeker"
	"github.com/sunfish-shogi/bufseekio"
)

type mp4Writer interface {
	Write(p []byte) (int, error)
	Seek(offset int64, whence int) (int64, error)
	StartBox(bi *mp4lib.BoxInfo) (*mp4lib.BoxInfo, error)
	EndBox() (*mp4lib.BoxInfo, error)
	CopyBox(r io.ReadSeeker, bi *mp4lib.BoxInfo) error
}

type mp4WriteSeeker interface {
	Write(p []byte) (int, error)
	Seek(offset int64, whence int) (int64, error)
	Bytes() []byte
}

// Make new atoms and write to.
func createAndWrite(w mp4Writer, ctx mp4lib.Context, _tags *MP4Tag, fields map[mp4lib.BoxType]string) error {
	var boxData *mp4lib.Data
	dataCtx := ctx
	dataCtx.UnderIlstMeta = true
	keys := make([]mp4lib.BoxType, 0, len(fields))
	for boxType := range fields {
		keys = append(keys, boxType)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	for _, boxType := range keys {
		tagName := fields[boxType]
		switch tagName {
		case "BPM":
			if _tags.BPM == 0 {
				continue
			}
			buf := make([]byte, 2)
			if _tags.BPM <= 32767 {
				binary.BigEndian.PutUint16(buf, uint16(_tags.BPM))
			} else {
				buf = encodedUint32(uint32(_tags.BPM))
			}
			boxData = &mp4lib.Data{
				DataType: mp4lib.DataTypeSignedIntBigEndian,
				Data:     buf,
			}

		case "TrackNumber", "DiscNumber":
			total := strings.ReplaceAll(tagName, "Number", "Total")
			numVal := reflect.ValueOf(*_tags).FieldByName(tagName).Int()
			totalVal := reflect.ValueOf(*_tags).FieldByName(total).Int()
			if numVal == 0 && totalVal == 0 {
				continue
			}
			buf := make([]byte, 2)
			temp := make([]byte, 2)
			binary.BigEndian.PutUint16(temp, uint16(numVal))
			buf = append(buf, temp...)
			binary.BigEndian.PutUint16(temp, uint16(totalVal))
			buf = append(buf, temp...)
			if tagName == "TrackNumber" {
				buf = append(buf, 0, 0)
			}
			boxData = &mp4lib.Data{
				DataType: mp4lib.DataTypeBinary,
				Data:     buf,
			}

		case "CoverArt":
			if _tags.CoverArt == nil {
				continue
			}
			buf := new(bytes.Buffer)
			if err := png.Encode(buf, *_tags.CoverArt); err != nil {
				return err
			}
			boxData = &mp4lib.Data{
				DataType: 14, // iTunes artwork data type for PNG
				Data:     buf.Bytes(),
			}

		default:
			if reflect.ValueOf(*_tags).FieldByName(tagName).IsZero() {
				continue
			}
			val := reflect.ValueOf(*_tags).FieldByName(tagName).String()
			boxData = &mp4lib.Data{
				DataType: mp4lib.DataTypeStringUTF8,
				Data:     []byte(val),
			}
		}

		if _, err := w.StartBox(&mp4lib.BoxInfo{Type: boxType}); err != nil {
			return err
		}
		if _, err := w.StartBox(&mp4lib.BoxInfo{Type: mp4lib.BoxTypeData()}); err != nil {
			return err
		}

		if _, err := mp4lib.Marshal(w, boxData, dataCtx); err != nil {
			return err
		}

		if _, err := w.EndBox(); err != nil {
			return err
		}
		if _, err := w.EndBox(); err != nil {
			return err
		}
	}
	return nil

}

func containsAtom(boxType mp4lib.BoxType) mp4lib.BoxType {
	if _, ok := atomsMap[boxType]; ok {
		return boxType
	}
	return mp4lib.BoxType{}
}

func saveMP4(r io.ReadSeeker, wo io.Writer, w mp4Writer, ws mp4WriteSeeker, _tags *MP4Tag) error {
	var mdatOffsetDiff int64
	var mdatCount int
	var moovCount int
	var unsupportedOffsets bool
	var stcoOffsets []int64
	var ilstExists bool
	layout, err := inspectMP4(r)
	if err != nil {
		return err
	}
	changed, err := legacyChanges(r, _tags)
	if err != nil {
		return err
	}
	if len(changed) != 0 && layout.hasForeign && layout.meta.header == 0 {
		return errors.New("mp4meta: cannot add iTunes metadata alongside a foreign user data collection")
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return err
	}
	rs := bufseekio.NewReadSeeker(r, 1024*1024, 4)

	_, err = mp4lib.ReadBoxStructure(rs, func(h *mp4lib.ReadHandle) (interface{}, error) {
		switch h.BoxInfo.Type {
		case mp4lib.BoxTypeMeta():
			if int64(h.BoxInfo.Offset) != layout.meta.start || layout.meta.header == 0 {
				return nil, w.CopyBox(r, &h.BoxInfo)
			}
		case mp4lib.BoxTypeIlst():
			if int64(h.BoxInfo.Offset) != layout.ilst.start || layout.ilst.header == 0 {
				return nil, w.CopyBox(r, &h.BoxInfo)
			}
		}
		switch h.BoxInfo.Type {
		case mp4lib.BoxTypeMoof():
			return nil, errors.New("mp4meta: fragmented MP4 is not supported")
		case mp4lib.BoxTypeCo64():
			unsupportedOffsets = true
			if err := w.CopyBox(r, &h.BoxInfo); err != nil {
				return nil, err
			}
			return nil, nil
		// 1. moov, trak, mdia, minf, stbl, udta
		case mp4lib.BoxTypeMoov(),
			mp4lib.BoxTypeTrak(),
			mp4lib.BoxTypeMdia(),
			mp4lib.BoxTypeMinf(),
			mp4lib.BoxTypeStbl(),
			mp4lib.BoxTypeUdta(),
			mp4lib.BoxTypeMeta(),
			mp4lib.BoxTypeIlst():
			if h.BoxInfo.Type == mp4lib.BoxTypeMoov() {
				moovCount++
			}
			_, err := w.StartBox(&h.BoxInfo)
			if err != nil {
				return nil, err
			}
			if h.BoxInfo.Type == mp4lib.BoxTypeMeta() {
				meta, n, err := h.ReadPayload()
				if err != nil {
					return nil, err
				}
				if n != 0 {
					if _, err := mp4lib.Marshal(w, meta, h.BoxInfo.Context); err != nil {
						return nil, err
					}
				}
			}
			if h.BoxInfo.Type != mp4lib.BoxTypeIlst() {
				if _, err := h.Expand(); err != nil {
					return nil, err
				}
			}
			// 1-a. [only moov box] add udta box if not exists
			if h.BoxInfo.Type == mp4lib.BoxTypeMoov() && !ilstExists && len(changed) != 0 {
				path := []mp4lib.BoxType{mp4lib.BoxTypeUdta(), mp4lib.BoxTypeMeta()}
				for _, boxType := range path {
					if _, err := w.StartBox(&mp4lib.BoxInfo{Type: boxType}); err != nil {
						return nil, err
					}
				}
				ctx := h.BoxInfo.Context
				ctx.UnderUdta = true
				if _, err := mp4lib.Marshal(w, &mp4lib.Meta{}, ctx); err != nil {
					return nil, err
				}
				if _, err := w.StartBox(&mp4lib.BoxInfo{Type: mp4lib.BoxTypeHdlr()}); err != nil {
					return nil, err
				}
				hdlr := &mp4lib.Hdlr{
					HandlerType: [4]byte{'m', 'd', 'i', 'r'},
				}
				if _, err := mp4lib.Marshal(w, hdlr, ctx); err != nil {
					return nil, err
				}
				if _, err := w.EndBox(); err != nil {
					return nil, err
				}
				if _, err := w.StartBox(&mp4lib.BoxInfo{Type: mp4lib.BoxTypeIlst()}); err != nil {
					return nil, err
				}
				ctx.UnderIlst = true
				if err := createAndWrite(w, ctx, _tags, changed); err != nil {
					return nil, err
				}
				if _, err := w.EndBox(); err != nil {
					return nil, err
				}
				for range path {
					if _, err := w.EndBox(); err != nil {
						return nil, err
					}
				}
			}
			// 1-b. [only ilst box] add metadatas
			if h.BoxInfo.Type == mp4lib.BoxTypeIlst() {
				ctx := h.BoxInfo.Context
				ctx.UnderIlst = true
				var tail *mp4lib.BoxInfo
				for _, item := range layout.items {
					itemBox := itemInfo(item.box)
					if _, modified := changed[itemBox.Type]; modified {
						continue
					}
					if item.box.extendsTail {
						tail = itemBox
					} else if err := w.CopyBox(r, itemBox); err != nil {
						return nil, err
					}
				}
				if err := createAndWrite(w, ctx, _tags, changed); err != nil {
					return nil, err
				}
				if tail != nil {
					if err := w.CopyBox(r, tail); err != nil {
						return nil, err
					}
				}
				ilstExists = true
			}
			if _, err = w.EndBox(); err != nil {
				return nil, err
			}
		// 2. otherwise
		default:
			// 2-a. [only stco box] keep offset
			if h.BoxInfo.Type == mp4lib.BoxTypeStco() {
				offset, err := w.Seek(0, io.SeekCurrent)
				if err != nil {
					return nil, err
				}
				stcoOffsets = append(stcoOffsets, offset)
			}
			// 2-b. [only mdat box] keep difference of offsets
			if h.BoxInfo.Type == mp4lib.BoxTypeMdat() {
				iOffset := int64(h.BoxInfo.Offset)
				mdatCount++
				oOffset, err := w.Seek(0, io.SeekCurrent)
				if err != nil {
					return nil, err
				}
				mdatOffsetDiff = oOffset - iOffset
			}
			// copy box without modification
			if err := w.CopyBox(r, &h.BoxInfo); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	if err != nil {
		return err
	}

	if moovCount != 1 || mdatCount != 1 {
		return errors.New("mp4meta: expected exactly one moov and one mdat box")
	}
	if unsupportedOffsets && mdatOffsetDiff != 0 {
		return errors.New("mp4meta: cannot relocate co64 offsets")
	}
	if delta := int64(len(ws.Bytes())) - layout.size; delta != 0 {
		if _, err := layout.relocateChunks(nil, layout.moov.end, delta); err != nil {
			return err
		}
	}
	ts := bufseekio.NewReadSeeker(bytes.NewReader(ws.Bytes()), 1024*1024, 3)

	if _, err = ws.Seek(0, io.SeekStart); err != nil {
		return err
	}
	// if mdat box is moved, update stco box
	if mdatOffsetDiff != 0 {
		for _, stcoOffset := range stcoOffsets {
			// seek to stco box header
			if _, err := ts.Seek(stcoOffset, io.SeekStart); err != nil {
				return err
			}
			// read box header
			bi, err := mp4lib.ReadBoxInfo(ts)
			if err != nil {
				return err
			}
			// read stco box payload
			var stco mp4lib.Stco
			_, err = mp4lib.Unmarshal(ts, bi.Size-bi.HeaderSize, &stco, bi.Context)
			if err != nil {
				return err
			}
			// update chunk offsets
			for i := range stco.ChunkOffset {
				next := int64(stco.ChunkOffset[i]) + mdatOffsetDiff
				if next < 0 || next > int64(^uint32(0)) {
					return errors.New("mp4meta: chunk offset is out of range")
				}
				stco.ChunkOffset[i] = uint32(next)
			}
			// seek to stco box payload
			_, err = bi.SeekToPayload(ws)
			if err != nil {
				return err
			}
			// write stco box payload
			if _, err := mp4lib.Marshal(ws, &stco, bi.Context); err != nil {
				return err
			}
		}
	}
	if _, err = ws.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if reflect.TypeOf(wo) == reflect.TypeOf(new(os.File)) {
		f := wo.(*os.File)
		path, err := filepath.Abs(f.Name())
		if err != nil {
			return err
		}
		w2, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			return err
		}
		defer w2.Close()
		if _, err = w2.Write(ws.Bytes()); err != nil {
			return err
		}
		if _, err = f.Seek(0, io.SeekEnd); err != nil {
			return err
		}
		return nil
	}
	_, err = io.Copy(wo, bytes.NewReader(ws.Bytes()))
	return err

}

// SaveMP4 rewrites supported metadata in a non-fragmented MP4. It buffers the result before writing.
func SaveMP4(r io.ReadSeeker, wo io.Writer, _tags *MP4Tag) error {
	if r == nil || wo == nil || _tags == nil {
		return errors.New("mp4meta: source, destination and tags are required")
	}
	for _, item := range []struct {
		name  string
		value int
	}{{"BPM", _tags.BPM}, {"track number", _tags.TrackNumber}, {"track total", _tags.TrackTotal}, {"disc number", _tags.DiscNumber}, {"disc total", _tags.DiscTotal}} {
		if item.value < 0 || item.value > 65535 {
			return fmt.Errorf("mp4meta: %s must be between 0 and 65535", item.name)
		}
	}
	if _tags.CoverArt != nil && *_tags.CoverArt == nil {
		return errors.New("mp4meta: nil cover image")
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return err
	}
	ws := &writerseeker.WriterSeeker{}
	defer ws.Close()
	w := mp4lib.NewWriter(ws)
	return saveMP4(r, wo, w, ws, _tags)
}

func itemInfo(b boxRange) *mp4lib.BoxInfo {
	result := &mp4lib.BoxInfo{Offset: uint64(b.start), Size: uint64(b.size()), HeaderSize: uint64(b.header), ExtendToEOF: b.extendsTail}
	copy(result.Type[:], b.kind)
	return result
}

// legacyChanges recognizes direct edits to exported fields as well as setter
// calls. Raw artwork is retained when the decoded pixels have not changed.
func legacyChanges(source io.ReadSeeker, edited *MP4Tag) (map[mp4lib.BoxType]string, error) {
	if edited.original == nil || edited.cleared || !sameReader(source, edited.reader) {
		return atomsMap, nil
	}
	current, before := reflect.ValueOf(edited).Elem(), reflect.ValueOf(edited.original).Elem()
	changed := make(map[mp4lib.BoxType]string)
	for key, name := range atomsMap {
		switch name {
		case "CoverArt":
			if edited.CoverArt == nil {
				if edited.original.CoverArt == nil {
					continue
				}
			} else if edited.originalCover != nil {
				decoded, _, err := image.Decode(bytes.NewReader(*edited.originalCover))
				if err != nil {
					return nil, err
				}
				if reflect.DeepEqual(decoded, *edited.CoverArt) {
					continue
				}
			}
		case "TrackNumber", "DiscNumber":
			total := strings.ReplaceAll(name, "Number", "Total")
			if current.FieldByName(name).Int() == before.FieldByName(name).Int() &&
				current.FieldByName(total).Int() == before.FieldByName(total).Int() {
				continue
			}
		default:
			if current.FieldByName(name).Interface() == before.FieldByName(name).Interface() {
				continue
			}
		}
		changed[key] = name
	}
	return changed, nil
}

func sameReader(a, b io.ReadSeeker) bool {
	left, right := reflect.ValueOf(a), reflect.ValueOf(b)
	return left.IsValid() && right.IsValid() && left.Type() == right.Type() &&
		left.Kind() == reflect.Ptr && left.Pointer() == right.Pointer()
}
