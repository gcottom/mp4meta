package mp4meta

import (
	"bytes"
	"encoding/binary"
	"image"
	"io"
	"reflect"

	mp4lib "github.com/abema/go-mp4"
	"github.com/sunfish-shogi/bufseekio"
)

func ReadMP4(reader io.ReadSeeker) (*MP4Tag, error) {
	tag := new(MP4Tag)
	tag.reader = reader
	tptr := reflect.ValueOf(tag).Elem()
	r := bufseekio.NewReadSeeker(reader, 1024*1024, 4)
	var ptyp mp4lib.BoxType
	var field string
	_, err := mp4lib.ReadBoxStructure(r, func(h *mp4lib.ReadHandle) (val interface{}, err error) {
		switch h.BoxInfo.Type {
		case mp4lib.BoxTypeMoov(), mp4lib.BoxTypeUdta(), mp4lib.BoxTypeMeta(), mp4lib.BoxTypeIlst():
			return h.Expand()
		case containsAtom(h.BoxInfo.Type):
			ptyp = h.BoxInfo.Type
			field = atomsMap[ptyp]
			return h.Expand()
		case mp4lib.BoxTypeData():
			ib, n, err := h.ReadPayload()
			valueSize := n - 8
			if err != nil || valueSize < 1 {
				return nil, err
			}
			data := ib.(*mp4lib.Data)
			switch ptyp {
			case mp4lib.BoxType{'t', 'r', 'k', 'n'}, mp4lib.BoxType{'d', 'i', 's', 'k'}:
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
				// Win7 Explorer for BPM<256 write int8u:
				// | | | | BeatsPerMinute = 120
				// | | | | - Tag 'tmpo', Type='data', Flags=0x15 (signed int), Lang=0x0000 (1 bytes, int8u):
				// | | | | 29a00e84: 78
				// mp3tag write always int16u:                                           [x]
				// | | | | BeatsPerMinute = 120
				// | | | | - Tag 'tmpo', Type='data', Flags=0x15 (signed int), Lang=0x0000 (2 bytes, int16u):
				// | | | |    334f8: 00 78                                           [.x]
				tag.BPM = getInt(data.Data[:valueSize])
				return nil, nil
			case mp4lib.BoxType{'g', 'n', 'r', 'e'}:
				// | | | | Genre = !
				// | | | | - Tag 'gnre', Type='data', Flags=0x0 (undef), Lang=0x0000 (2 bytes, undef):
				// | | | | 29a00e9d: 00 21                                           [.!]
				if tag.Genre == "" { // give priority to (c)gen
					tag.Genre = Id3v1GenreStr[getInt(data.Data[:valueSize])-1]
				}
				return nil, nil
			case mp4lib.BoxType{'c', 'o', 'v', 'r'}:
				img, _, err := image.Decode(bytes.NewReader(data.Data))
				if err != nil {
					return nil, err
				}
				tag.CoverArt = &img
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
	return tag, nil
}
