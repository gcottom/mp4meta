package mp4meta

import (
	"bytes"

	core "github.com/gcottom/audiometa-core"
)

func coverMIME(code uint32) string {
	switch code {
	case mp4JPEG:
		return "image/jpeg"
	case mp4PNG:
		return "image/png"
	}
	return ""
}

func coverCell(p core.Picture) (metadataCell, bool) {
	var code uint32
	switch p.MIME {
	case "image/jpeg":
		code = mp4JPEG
	case "image/png":
		code = mp4PNG
	default:
		return metadataCell{}, false
	}
	if !pictureFormat(p.MIME, p.Data) {
		return metadataCell{}, false
	}
	return metadataCell{code, p.Data}, true
}

// The namespaced artwork array supplies richer attributes only if the readable
// covr projection still matches it. This leaves other editors free to replace
// the conventional artwork.
func artworkExtension(cells map[string][]metadataCell) []core.Value {
	native := cells["covr"]
	if len(native) == 0 {
		return nil // the usual fallback read already loaded any private artwork
	}
	stored := cells[fallbackField(core.Artwork)]
	if len(stored) == 0 {
		return nil
	}
	var full []core.Value
	var i int
	for _, cell := range stored {
		text, ok := readText(cell)
		if !ok {
			return nil
		}
		v, err := core.Decode(core.Artwork, text)
		if err != nil {
			return nil
		}
		full = append(full, v)
		picture, _ := v.Picture()
		if p, ok := coverCell(picture); ok {
			if i == len(native) || p.code != native[i].code || !bytes.Equal(p.data, native[i].data) {
				return nil
			}
			i++
		}
	}
	if i != len(native) {
		return nil
	}
	return full
}

func (c itemChanges) setArtwork(values []core.Value) error {
	fallback := fallbackField(core.Artwork)
	c[fallback] = nil
	var projection []metadataCell
	rich := false
	for _, value := range values {
		p, _ := value.Picture()
		if cell, ok := coverCell(p); ok {
			projection = append(projection, cell)
			rich = rich || p.Role != 0 || p.Description != "" || p.Width != 0 ||
				p.Height != 0 || p.Depth != 0 || p.Colors != 0
		} else {
			rich = true
		}
	}
	if rich {
		cells := make([]metadataCell, 0, len(values))
		for _, value := range values {
			text, err := core.Encode(core.Artwork, value)
			if err != nil {
				return err
			}
			cells = append(cells, metadataCell{mp4Text, []byte(text)})
		}
		if err := c.set(fallback, cells); err != nil {
			return err
		}
	}
	return c.set("covr", projection)
}
