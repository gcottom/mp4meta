package mp4meta

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"

	core "github.com/gcottom/audiometa-core"
)

type itemChanges map[string][]byte

func (l *mp4Layout) save(dst io.Writer, original map[core.Field][]core.Value, edits core.Changes) error {
	if edits.Empty() {
		return l.writePatches(dst, nil)
	}
	if l.fragmented {
		return fmt.Errorf("%w: editing fragmented MP4", core.ErrUnsupported)
	}
	canonical := make(itemChanges)
	for _, def := range core.Fields() {
		values, changed := edits.Fields[def.ID]
		if !changed || pairField(def.ID) || def.ID == core.Chapters {
			continue
		}
		keys := mappingKeys(def)
		if len(keys) == 0 || !def.Mutable {
			return fmt.Errorf("%w: %s", core.ErrUnsupported, def.ID)
		}
		if def.ID == core.Artwork {
			if err := canonical.setArtwork(values); err != nil {
				return err
			}
			continue
		}
		if err := canonical.setField(def, keys, values); err != nil {
			return err
		}
	}
	if err := canonical.setPair("trkn", core.TrackNumber, core.TrackTotal, original, edits); err != nil {
		return err
	}
	if err := canonical.setPair("disk", core.DiscNumber, core.DiscTotal, original, edits); err != nil {
		return err
	}

	chapters, chapterEdit, err := l.planChapters(canonical, edits)
	if err != nil {
		return err
	}
	for name, values := range edits.Custom {
		key := freeformKey(core.MP4Namespace, core.CustomName(name))
		cells := make([]metadataCell, len(values))
		for i, text := range values {
			cells[i] = metadataCell{mp4Text, []byte(text)}
		}
		if err := canonical.set(key, cells); err != nil {
			return err
		}
	}

	sizes := make(map[boxRange]int64)
	patches, newMeta, err := l.patchItems(canonical, edits.ClearUnknown, sizes)
	if err != nil {
		return err
	}
	newUserData := newMeta
	if chapterEdit {
		if l.chpl.header != 0 {
			patches = append(patches, replacement(l.chpl, chapters))
			sizes[l.chplParent] += int64(len(chapters)) - l.chpl.size()
		} else {
			newUserData = append(newUserData, chapters...)
		}
	}
	if len(newUserData) != 0 {
		switch {
		case l.udta.header == 0:
			data := makeBox("udta", newUserData)
			patches = append(patches, filePatch{l.moov.payload(), l.moov.payload(), data})
			sizes[l.moov] += int64(len(data))
		default:
			patches = append(patches, filePatch{l.udta.payload(), l.udta.payload(), newUserData})
			sizes[l.udta] += int64(len(newUserData))
		}
	}
	for parent, delta := range sizes {
		if parent.kind == "udta" {
			sizes[l.moov] += delta
		}
	}
	for parent, delta := range sizes {
		p, err := changedSize(parent, delta)
		if err != nil {
			return err
		}
		if p.data != nil {
			patches = append(patches, p)
		}
	}
	patches, err = l.relocateChunks(patches, l.moov.end, sizes[l.moov])
	if err != nil {
		return err
	}
	return l.writePatches(dst, patches)
}

func (c itemChanges) set(key string, cells []metadataCell) error {
	data, err := newItem(key, cells)
	if err != nil {
		return err
	}
	c[key] = data
	return nil
}

func (c itemChanges) setField(def core.Definition, keys []string, values []core.Value) error {
	key, cells, err := writeCells(def, keys[0], values)
	if err != nil {
		return err
	}
	for _, candidate := range keys {
		// Replacing the primary keeps conflicting aliases and old fallback
		// values. Removal and switching to fallback must remove shadows.
		if len(values) == 0 || key != keys[0] {
			c[candidate] = nil
		}
	}
	return c.set(key, cells)
}

func pairField(id core.Field) bool {
	switch id {
	case core.TrackNumber, core.TrackTotal, core.DiscNumber, core.DiscTotal:
		return true
	}
	return false
}

func (c itemChanges) setPair(key string, first, second core.Field, original map[core.Field][]core.Value, edits core.Changes) error {
	_, firstEdited := edits.Fields[first]
	_, secondEdited := edits.Fields[second]
	if !firstEdited && !secondEdited {
		return nil
	}
	size := 6
	if key == "trkn" {
		size = 8
	}
	pair := make([]byte, size)
	hasPair := false
	for i, field := range []core.Field{first, second} {
		fallback := fallbackField(field)
		values, edited := edits.Fields[field]
		if edited {
			c[fallback] = nil
		} else {
			values = original[field]
		}
		if len(values) == 0 {
			continue
		}
		v := values[0]
		n, ok := v.Int()
		if !ok || n < 0 {
			return fmt.Errorf("mp4meta: invalid %s", field)
		}
		if n == 0 || n > math.MaxUint16 {
			if !edited {
				continue
			}
			// Zero in these atoms means unknown. Oversized and explicitly
			// zero values retain their meaning in field-specific storage.
			text, err := core.Encode(field, v)
			if err != nil {
				return err
			}
			if err := c.set(fallback, []metadataCell{{mp4Text, []byte(text)}}); err != nil {
				return err
			}
			continue
		}
		binary.BigEndian.PutUint16(pair[2+i*2:], uint16(n))
		hasPair = true
	}
	c[key] = nil
	if hasPair {
		return c.set(key, []metadataCell{{0, pair}})
	}
	return nil
}

func (l *mp4Layout) patchItems(changes itemChanges, clearUnknown bool, sizes map[boxRange]int64) ([]filePatch, []byte, error) {
	var patches []filePatch
	var delta int64
	insertion := l.ilst.end
	for _, item := range l.items {
		data, changed := changes[item.key]
		if changed {
			changes[item.key] = nil // only emit a replacement at the first occurrence
		}
		if changed || clearUnknown && !keepOnClear(item.key) {
			p := replacement(item.box, data)
			delta += p.delta()
			patches = append(patches, p)
		} else if item.box.extendsTail {
			insertion = item.box.start
		}
	}
	additions, err := remainingItems(changes, l.ilst.size()+delta)
	if err != nil {
		return nil, nil, err
	}
	if l.ilst.header != 0 {
		if len(additions) != 0 {
			patches = append(patches, filePatch{insertion, insertion, additions})
			delta += int64(len(additions))
		}
		sizes[l.ilst] += delta
		sizes[l.meta] += delta
		sizes[l.udta] += delta
		return patches, nil, nil
	}
	if len(additions) == 0 {
		return patches, nil, nil
	}
	ilst := makeBox("ilst", additions)
	if l.meta.header != 0 {
		insertion = l.metaInsert
		patches = append(patches, filePatch{insertion, insertion, ilst})
		sizes[l.meta] += int64(len(ilst))
		sizes[l.udta] += int64(len(ilst))
		return patches, nil, nil
	}
	if l.hasForeign {
		return nil, nil, fmt.Errorf("%w: adding iTunes metadata alongside a foreign movie-level user data collection", core.ErrUnsupported)
	}
	handler := make([]byte, 25)
	copy(handler[8:], "mdirappl")
	return patches, makeBox("meta", make([]byte, 4), makeBox("hdlr", handler), ilst), nil
}

func remainingItems(items itemChanges, existingSize int64) ([]byte, error) {
	var keys []string
	total := int64(0)
	for key, data := range items {
		if len(data) != 0 {
			keys = append(keys, key)
			total += int64(len(data))
		}
	}
	if total+existingSize > maxMetadataBytes {
		return nil, errors.New("mp4meta: iTunes item list exceeds 64 MiB")
	}
	sort.Strings(keys)
	data := make([]byte, 0, total)
	for _, key := range keys {
		data = append(data, items[key]...)
	}
	return data, nil
}

func keepOnClear(key string) bool {
	switch key {
	case itunesTrim, "----:com.apple.iTunes:iTunNORM", "----:com.apple.iTunes:iTunPGAP":
		return true
	}
	return false
}
