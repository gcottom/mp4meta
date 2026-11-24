package mp4meta

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"io"
	"reflect"

	mp4lib "github.com/abema/go-mp4"
	"github.com/sunfish-shogi/bufseekio"
)

// ReadMP4 reads supported iTunes-style metadata from the start of a seekable MP4.
func ReadMP4(reader io.ReadSeeker) (*MP4Tag, error) {
	if reader == nil {
		return nil, errors.New("mp4meta: nil source")
	}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	tag := new(MP4Tag)
	tag.reader = reader
	tptr := reflect.ValueOf(tag).Elem()
	r := bufseekio.NewReadSeeker(reader, 1024*1024, 4)
	var foundMovie bool
	var ptyp mp4lib.BoxType
	var field string
	_, err := mp4lib.ReadBoxStructure(r, func(h *mp4lib.ReadHandle) (val interface{}, err error) {
		switch h.BoxInfo.Type {
		case mp4lib.BoxTypeMoov():
			foundMovie = true
			return h.Expand()
		case mp4lib.BoxTypeUdta(), mp4lib.BoxTypeMeta(), mp4lib.BoxTypeIlst():
			return h.Expand()
		case containsAtom(h.BoxInfo.Type):
			ptyp = h.BoxInfo.Type
			field = atomsMap[ptyp]
			v, err := h.Expand()
			ptyp = mp4lib.BoxType{}
			field = ""
			return v, err
		case mp4lib.BoxTypeData():
			if field == "" {
				return nil, nil
			}
			ib, _, err := h.ReadPayload()
			if err != nil {
				return nil, err
			}
			data := ib.(*mp4lib.Data)
			switch ptyp {
			case mp4lib.BoxType{'t', 'r', 'k', 'n'}, mp4lib.BoxType{'d', 'i', 's', 'k'}:
				if len(data.Data) < 6 {
					return nil, fmt.Errorf("mp4meta: short %s value", field)
				}
				var num uint16
				if err := binary.Read(bytes.NewReader(data.Data[2:4]), binary.BigEndian, &num); err != nil {
					return nil, err
				}
				tptr.FieldByName(field).SetInt(int64(num))
				typ := reflect.TypeOf(*tag)
				fNum := 0
			strL:
				for i := 0; i < typ.NumField(); i++ {
					if typ.Field(i).Name == field {
						fNum = i + 1
						break strL
					}
				}
				if err = binary.Read(bytes.NewReader(data.Data[4:6]), binary.BigEndian, &num); err != nil {
					return nil, err
				}
				tptr.Field(fNum).SetInt(int64(num))
				return nil, nil
			case mp4lib.BoxType{'t', 'm', 'p', 'o'}:
				if len(data.Data) < 2 {
					return nil, errors.New("mp4meta: short BPM value")
				}
				tag.BPM = getInt(data.Data[:2])
				if len(data.Data) == 4 && !legacyTempoPadding(data.Data) {
					tag.BPM = getInt(data.Data)
				}
				return nil, nil
			case mp4lib.BoxType{'c', 'o', 'v', 'r'}:
				img, _, err := image.Decode(bytes.NewReader(data.Data))
				if err != nil {
					return nil, err
				}
				tag.CoverArt = &img
				tag.originalCover = &data.Data
				return nil, nil
			case mp4lib.BoxType{'\251', 'a', 'l', 'b'}, mp4lib.BoxType{'a', 'A', 'R', 'T'}, mp4lib.BoxType{'\251', 'A', 'R', 'T'}, mp4lib.BoxType{'\251', 'c', 'm', 't'}, mp4lib.BoxType{'\251', 'w', 'r', 't'}, mp4lib.BoxType{'c', 'p', 'r', 't'}, mp4lib.BoxType{'\251', 'g', 'e', 'n'}, mp4lib.BoxType{'\251', 'n', 'a', 'm'}, mp4lib.BoxType{'\251', 'd', 'a', 'y'}, mp4lib.BoxType{'\251', 't', 'o', 'o'}:
				if reflect.ValueOf(string(data.Data)).IsZero() {
					return nil, nil
				} else {
					tptr.FieldByName(field).SetString(string(data.Data))
				}

				return nil, nil
			default:
				return nil, nil
			}
		}
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	if !foundMovie {
		return nil, errors.New("mp4meta: missing moov box")
	}
	loaded := *tag
	loaded.originalCover = nil
	tag.original = &loaded
	return tag, nil
}
