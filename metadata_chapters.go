package mp4meta

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"unicode/utf8"

	core "github.com/gcottom/audiometa-core"
)

func (l *mp4Layout) readExtraFields(fields map[core.Field][]core.Value) error {
	if l.chpl.header == 0 {
		return nil
	}
	n := l.chpl.end - l.chpl.payload()
	if n < 5 || n > 9+255*(9+255) {
		return nil
	}
	data := make([]byte, n)
	if _, err := l.at.ReadAt(data, l.chpl.payload()); err != nil {
		return err
	}
	var i int
	switch data[0] {
	case 0:
		i = 4
	case 1:
		i = 8
	default:
		return nil
	}
	if i >= len(data) {
		return nil
	}
	count := int(data[i])
	data = data[i+1:]
	var chapters []core.Value
	for j := 0; j < count; j++ {
		if len(data) < 9 || int(data[8]) > len(data)-9 {
			return nil
		}
		start := binary.BigEndian.Uint64(data[:8])
		titleLen := int(data[8])
		c := core.Chapter{ID: chapterID(j), StartMS: int64(start / 10000), Title: string(data[9 : 9+titleLen])}
		data = data[9+titleLen:]
		// The canonical timeline is in milliseconds. Keep a finer
		// source timeline opaque instead of exposing rounded chapters.
		if start%10000 != 0 || !utf8.ValidString(c.Title) {
			return nil
		}
		v := core.ChapterValue(c)
		if err := core.Validate(core.Chapters, v); err != nil {
			return nil
		}
		chapters = append(chapters, v)
	}
	if len(chapters) != 0 && len(data) == 0 && !matchChapterProjection(chapters, fields[core.Chapters]) {
		fields[core.Chapters] = chapters
	}
	return nil
}

func (l *mp4Layout) planChapters(changes itemChanges, edits core.Changes) ([]byte, bool, error) {
	values, changed := edits.Fields[core.Chapters]
	if !changed {
		return nil, false, nil
	}
	if l.chapterTrack || l.chapterAmbiguous {
		return nil, false, fmt.Errorf("%w: editing QuickTime chapter tracks or multiple chapter lists", core.ErrUnsupported)
	}
	fallback := fallbackField(core.Chapters)
	changes[fallback] = nil
	if len(values) == 0 {
		return nil, l.chpl.header != 0, nil
	}
	data, exact := packChapters(values)
	if exact {
		return data, true, nil
	}
	var cells []metadataCell
	for _, value := range values {
		encoded, err := core.Encode(core.Chapters, value)
		if err != nil {
			return nil, false, err
		}
		cells = append(cells, metadataCell{mp4Text, []byte(encoded)})
	}
	return data, data != nil || l.chpl.header != 0, changes.set(fallback, cells)
}

func chapterID(index int) string { return "chpl-" + strconv.Itoa(index+1) }

func packChapters(values []core.Value) ([]byte, bool) {
	if len(values) > 255 {
		return nil, false
	}
	data := make([]byte, 9)
	data[0], data[8] = 1, byte(len(values))
	previous := int64(-1)
	exact := true
	for i, value := range values {
		c, ok := value.Chapter()
		if !ok || c.StartMS <= previous ||
			c.StartMS > math.MaxUint64/10000 || len(c.Title) > 255 {
			return nil, false
		}
		exact = exact && c.ID == chapterID(i) && c.EndMS == 0
		previous = c.StartMS
		data = append(data, encodedUint64(uint64(c.StartMS)*10000)...)
		data = append(data, byte(len(c.Title)))
		data = append(data, c.Title...)
	}
	return makeBox("chpl", data), exact
}

func matchChapterProjection(native, full []core.Value) bool {
	if len(native) != len(full) {
		return false
	}
	for i, value := range native {
		chapter, _ := value.Chapter()
		extended, ok := full[i].Chapter()
		if !ok || chapter.StartMS != extended.StartMS || chapter.Title != extended.Title {
			return false
		}
	}
	return true
}
