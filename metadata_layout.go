package mp4meta

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"

	core "github.com/gcottom/audiometa-core/v2"
)

const (
	maxMetadataBytes = 64 << 20
	maxRecordedBoxes = 1 << 20
)

var errInvalidMP4Box = errors.New("mp4meta: invalid box structure")

type boxRange struct {
	kind        string
	start       int64
	end         int64
	header      int64
	extendsTail bool
}

func (b boxRange) payload() int64 { return b.start + b.header }
func (b boxRange) size() int64    { return b.end - b.start }

type seekerAt struct{ source io.ReadSeeker }

func (r seekerAt) ReadAt(dst []byte, offset int64) (int, error) {
	if _, err := r.source.Seek(offset, io.SeekStart); err != nil {
		return 0, err
	}
	return io.ReadFull(r.source, dst)
}

type mp4Layout struct {
	source           io.ReadSeeker
	at               io.ReaderAt
	size             int64
	moov             boxRange
	udta             boxRange
	meta             boxRange
	ilst             boxRange
	media            []boxRange
	items            []metadataItem
	metaInsert       int64
	chpl             boxRange
	chplParent       boxRange
	chapterTrack     bool
	chapterAmbiguous bool
	hasForeign       bool
	fragmented       bool
	externalIdx      bool
}

func inspectMP4(source io.ReadSeeker) (*mp4Layout, error) {
	if source == nil {
		return nil, errors.New("mp4meta: nil source")
	}
	n, err := source.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}
	at, ok := source.(io.ReaderAt)
	if !ok {
		at = seekerAt{source}
	}
	l := &mp4Layout{source: source, at: at, size: n}
	err = walkBoxes(at, 0, n, func(b boxRange) error {
		switch b.kind {
		case "moov":
			if l.moov.header != 0 {
				return errors.New("mp4meta: multiple moov boxes")
			}
			l.moov = b
		case "mdat":
			l.media = append(l.media, b)
		case "moof", "mfra":
			l.fragmented = true
		case "sidx", "ssix", "meta":
			l.externalIdx = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if l.moov.header == 0 || len(l.media) == 0 {
		return nil, errors.New("mp4meta: missing moov or mdat box")
	}
	err = walkBoxes(at, l.moov.payload(), l.moov.end, func(b boxRange) error {
		switch b.kind {
		case "udta":
			return l.inspectUserData(b)
		case "trak":
			return l.inspectTrack(b)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if l.ilst.header != 0 {
		if l.ilst.size() > maxMetadataBytes {
			return nil, errors.New("mp4meta: iTunes item list exceeds 64 MiB")
		}
		if err := l.indexItems(); err != nil {
			return nil, err
		}
	}
	return l, nil
}

func (l *mp4Layout) inspectUserData(udta boxRange) error {
	var hasForeign bool
	err := walkBoxes(l.at, udta.payload(), udta.end, func(b boxRange) error {
		if b.kind == "chpl" {
			if l.chpl.header == 0 {
				l.chpl, l.chplParent = b, udta
			} else {
				l.chapterAmbiguous = true
			}
			return nil
		}
		if b.kind != "meta" {
			return nil
		}
		handler, items, insertion, err := l.inspectMeta(b)
		if err != nil {
			return err
		}
		if handler != "mdir" {
			hasForeign = true
			return nil
		}
		if l.meta.header != 0 {
			return errors.New("mp4meta: ambiguous movie-level iTunes metadata")
		}
		l.udta, l.meta, l.ilst, l.metaInsert = udta, b, items, insertion
		return nil
	})
	if err != nil {
		return err
	}
	if l.meta.header == 0 && (l.udta.header == 0 || l.hasForeign && !hasForeign) {
		l.udta, l.hasForeign = udta, hasForeign
	}
	return nil
}

// An iTunes meta FullBox starts with version/flags. Some QuickTime
// metadata boxes omit that word, so try both exact child boundaries.
func (l *mp4Layout) inspectMeta(meta boxRange) (string, boxRange, int64, error) {
	var firstErr error
	for _, prefix := range []int64{4, 0} {
		var handler string
		var items boxRange
		insertion := meta.end
		err := walkBoxes(l.at, meta.payload()+prefix, meta.end, func(b boxRange) error {
			if b.extendsTail {
				insertion = b.start
			}
			switch b.kind {
			case "hdlr":
				if handler != "" || b.end-b.payload() < 12 {
					return errors.New("mp4meta: malformed metadata handler")
				}
				var code [4]byte
				if _, err := l.at.ReadAt(code[:], b.payload()+8); err != nil {
					return err
				}
				handler = string(code[:])
			case "ilst":
				if items.header != 0 {
					return errors.New("mp4meta: duplicate metadata item lists")
				}
				items = b
			}
			return nil
		})
		if err == nil && handler != "" {
			return handler, items, insertion, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	// Other meta schemes are copied as opaque data. If they are the only
	// movie-level collection, adding iTunes metadata is rejected on Save.
	return "", boxRange{}, 0, firstErr
}

func readBox(r io.ReaderAt, start, limit int64) (boxRange, error) {
	b := boxRange{start: start, header: 8}
	if limit-start < b.header {
		return b, fmt.Errorf("%w: incomplete header at %d", errInvalidMP4Box, start)
	}
	var head [16]byte
	if _, err := r.ReadAt(head[:8], start); err != nil {
		return b, err
	}
	b.kind = string(head[4:8])
	size := uint64(binary.BigEndian.Uint32(head[:4]))
	switch size {
	case 1:
		b.header = 16
		if limit-start < b.header {
			return b, fmt.Errorf("%w: incomplete extended header at %d", errInvalidMP4Box, start)
		}
		if _, err := r.ReadAt(head[8:], start+8); err != nil {
			return b, err
		}
		size = binary.BigEndian.Uint64(head[8:])
	case 0:
		b.extendsTail = true
		size = uint64(limit - start)
	}
	if b.kind == "uuid" {
		b.header += 16
	}
	if size < uint64(b.header) || size > uint64(limit-start) {
		return b, fmt.Errorf("%w: invalid %q size at %d", errInvalidMP4Box, b.kind, start)
	}
	b.end = start + int64(size)
	return b, nil
}

func walkBoxes(r io.ReaderAt, start, end int64, visit func(boxRange) error) error {
	if start > end {
		return fmt.Errorf("%w: invalid container boundary", errInvalidMP4Box)
	}
	for n := 0; start < end; n++ {
		if n >= maxRecordedBoxes {
			return fmt.Errorf("%w: too many boxes", errInvalidMP4Box)
		}
		b, err := readBox(r, start, end)
		if err != nil {
			return err
		}
		if err := visit(b); err != nil {
			return err
		}
		start = b.end
	}
	return nil
}

type filePatch struct {
	start int64
	end   int64
	data  []byte
}

func replacement(b boxRange, data []byte) filePatch {
	return filePatch{b.start, b.end, data}
}

func (p filePatch) delta() int64 { return int64(len(p.data)) - (p.end - p.start) }

func changedSize(b boxRange, delta int64) (filePatch, error) {
	if b.extendsTail || delta == 0 {
		return filePatch{}, nil
	}
	size := b.size() + delta
	if size < b.header {
		return filePatch{}, errors.New("mp4meta: invalid resized box length")
	}
	if b.header == 16 {
		return filePatch{b.start + 8, b.start + 16, encodedUint64(uint64(size))}, nil
	}
	if size > math.MaxUint32 {
		return filePatch{}, errors.New("mp4meta: resized box needs an extended size header")
	}
	return filePatch{b.start, b.start + 4, encodedUint32(uint32(size))}, nil
}

func makeBox(kind string, children ...[]byte) []byte {
	n := 8
	for _, child := range children {
		n += len(child)
	}
	buf := make([]byte, 8, n)
	binary.BigEndian.PutUint32(buf, uint32(n))
	copy(buf[4:], kind)
	for _, child := range children {
		buf = append(buf, child...)
	}
	return buf
}

func (l *mp4Layout) relocateChunks(patches []filePatch, at, delta int64) ([]filePatch, error) {
	if delta == 0 {
		return patches, nil
	}
	if l.externalIdx || l.fragmented {
		return nil, fmt.Errorf("%w: MP4 resize with external item or segment indexes", core.ErrUnsupported)
	}
	mediaShifts := l.media[len(l.media)-1].start >= at
	var tablesBytes int64
	var scan func(int64, int64, int) error
	scan = func(start, end int64, depth int) error {
		return walkBoxes(l.at, start, end, func(b boxRange) error {
			switch b.kind {
			case "trak", "mdia", "minf", "stbl", "udta":
				if depth >= 8 {
					return errors.New("mp4meta: unsupported nesting in movie")
				}
				return scan(b.payload(), b.end, depth+1)
			case "meta":
				if err := l.checkMetadataOffsets(b); err != nil {
					return err
				}
			case "saio", "iloc", "sidx":
				return fmt.Errorf("%w: MP4 resize with %s offsets", core.ErrUnsupported, b.kind)
			case "stco", "co64":
				if !mediaShifts {
					return nil
				}
				tablesBytes += b.end - b.payload()
				if tablesBytes > maxMetadataBytes {
					return errors.New("mp4meta: chunk offset tables exceed 64 MiB")
				}
				p, err := l.relocateTable(b, at, delta)
				if err != nil {
					return err
				}
				patches = append(patches, p)
			}
			return nil
		})
	}
	if err := scan(l.moov.payload(), l.moov.end, 0); err != nil {
		return nil, err
	}
	return patches, nil
}

func (l *mp4Layout) relocateTable(b boxRange, at, delta int64) (filePatch, error) {
	payload := make([]byte, b.end-b.payload())
	if _, err := l.at.ReadAt(payload, b.payload()); err != nil {
		return filePatch{}, err
	}
	if len(payload) < 8 || binary.BigEndian.Uint32(payload[:4]) != 0 {
		return filePatch{}, fmt.Errorf("mp4meta: malformed %s table", b.kind)
	}
	width := 4
	if b.kind == "co64" {
		width = 8
	}
	n := int64(binary.BigEndian.Uint32(payload[4:8]))
	if n*int64(width) != int64(len(payload)-8) {
		return filePatch{}, fmt.Errorf("mp4meta: invalid %s entry count", b.kind)
	}
	for i := 8; i < len(payload); i += width {
		entry := payload[i : i+width]
		var offset uint64
		if width == 4 {
			offset = uint64(binary.BigEndian.Uint32(entry))
		} else {
			offset = binary.BigEndian.Uint64(entry)
		}
		index := sort.Search(len(l.media), func(i int) bool { return uint64(l.media[i].end) > offset })
		if index == len(l.media) || offset < uint64(l.media[index].payload()) {
			return filePatch{}, errors.New("mp4meta: chunk points outside local media")
		}
		if l.media[index].start < at {
			continue
		}
		if delta > 0 && int64(offset) > math.MaxInt64-delta {
			return filePatch{}, errors.New("mp4meta: chunk offset overflow")
		}
		next := int64(offset) + delta
		if next < 0 || width == 4 && next > math.MaxUint32 {
			return filePatch{}, fmt.Errorf("mp4meta: relocated %s offset out of range", b.kind)
		}
		if width == 4 {
			binary.BigEndian.PutUint32(entry, uint32(next))
		} else {
			binary.BigEndian.PutUint64(entry, uint64(next))
		}
	}
	return filePatch{b.payload(), b.end, payload}, nil
}

func (l *mp4Layout) writePatches(dst io.Writer, patches []filePatch) error {
	if src, ok := l.source.(*os.File); ok {
		if out, ok := dst.(*os.File); ok {
			srcInfo, err := src.Stat()
			if err != nil {
				return err
			}
			dstInfo, err := out.Stat()
			if err != nil {
				return err
			}
			if os.SameFile(srcInfo, dstInfo) {
				return errors.New("mp4meta: write metadata to a different file")
			}
		}
	}
	sort.SliceStable(patches, func(i, j int) bool {
		if patches[i].start == patches[j].start {
			return patches[i].end < patches[j].end
		}
		return patches[i].start < patches[j].start
	})
	var cursor int64
	for _, p := range patches {
		if p.start < cursor || p.end < p.start || p.end > l.size {
			return errors.New("mp4meta: overlapping or invalid output patches")
		}
		cursor = p.end
	}
	cursor = 0
	for _, p := range patches {
		if err := l.copyRange(dst, cursor, p.start); err != nil {
			return err
		}
		if len(p.data) != 0 {
			if _, err := io.Copy(dst, bytes.NewReader(p.data)); err != nil {
				return err
			}
		}
		cursor = p.end
	}
	return l.copyRange(dst, cursor, l.size)
}

func (l *mp4Layout) copyRange(dst io.Writer, start, end int64) error {
	if end == start {
		return nil
	}
	n, err := io.Copy(dst, io.NewSectionReader(l.at, start, end-start))
	if err == nil && n != end-start {
		return io.ErrUnexpectedEOF
	}
	return err
}

func (l *mp4Layout) inspectTrack(track boxRange) error {
	return walkBoxes(l.at, track.payload(), track.end, func(b boxRange) error {
		if b.kind != "tref" {
			return nil
		}
		return walkBoxes(l.at, b.payload(), b.end, func(ref boxRange) error {
			if ref.kind == "chap" {
				l.chapterTrack = true
			}
			return nil
		})
	})
}

func encodedUint32(n uint32) []byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], n)
	return b[:]
}

func encodedUint64(n uint64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], n)
	return b[:]
}

func (l *mp4Layout) checkMetadataOffsets(meta boxRange) error {
	for _, prefix := range []int64{4, 0} {
		unsafe := false
		err := walkBoxes(l.at, meta.payload()+prefix, meta.end, func(b boxRange) error {
			if b.kind == "iloc" || b.kind == "saio" {
				unsafe = true
			}
			return nil
		})
		if err == nil {
			if unsafe {
				return fmt.Errorf("%w: MP4 resize with offsets in a metadata collection", core.ErrUnsupported)
			}
			return nil
		}
	}
	return fmt.Errorf("%w: MP4 resize with malformed metadata collection", core.ErrUnsupported)
}
