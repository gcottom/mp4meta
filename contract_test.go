package mp4meta

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"io"
	"os"
	"strings"
	"testing"
)

type shortWriter struct{}

func (shortWriter) Write(b []byte) (int, error) { return len(b) / 2, nil }

func TestFastStartSavePreservesMedia(t *testing.T) {
	src, err := os.ReadFile("testdata/faststart-sine.m4a")
	if err != nil {
		t.Fatal(err)
	}
	tag, err := ReadMP4(bytes.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if tag.Title != "Before" {
		t.Fatalf("fixture title: %q", tag.Title)
	}
	tag.Title = strings.Repeat("長い題名 ", 256)
	img := image.Image(image.NewRGBA(image.Rect(0, 0, 2, 2)))
	tag.CoverArt = &img
	var dst bytes.Buffer
	if err := tag.Save(&dst); err != nil {
		t.Fatal(err)
	}
	reread, err := ReadMP4(bytes.NewReader(dst.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if reread.Title != tag.Title || reread.CoverArt == nil {
		t.Fatal("saved title/cover missing")
	}
	media := func(b []byte) (int, []byte) {
		for i := 0; i+8 <= len(b); {
			n := int(binary.BigEndian.Uint32(b[i : i+4]))
			if n < 8 || n > len(b)-i {
				break
			}
			if string(b[i+4:i+8]) == "mdat" {
				return i, b[i+8 : i+n]
			}
			i += n
		}
		t.Fatal("no mdat")
		return 0, nil
	}
	oldOff, oldMdat := media(src)
	newOff, newMdat := media(dst.Bytes())
	if newOff <= oldOff || !bytes.Equal(oldMdat, newMdat) {
		t.Fatal("mdat was not relocated intact")
	}
	// A single-track fast-start fixture has one stco entry pointing at mdat payload.
	offset := func(b []byte) int {
		i := bytes.Index(b, []byte("stco"))
		if i < 0 || i+16 > len(b) || binary.BigEndian.Uint32(b[i+8:i+12]) != 1 {
			t.Fatal("expected one chunk")
		}
		return int(binary.BigEndian.Uint32(b[i+12 : i+16]))
	}
	if offset(src) != oldOff+8 || offset(dst.Bytes()) != newOff+8 {
		t.Fatal("chunk offset not updated")
	}
	if err := tag.Save(shortWriter{}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short destination write: %v", err)
	}
}

func TestSaveInvalidTagLeavesDestinationUntouched(t *testing.T) {
	f, err := os.Open("testdata/faststart-sine.m4a")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	tag, err := ReadMP4(f)
	if err != nil {
		t.Fatal(err)
	}
	tag.SetTrackNumber(65536)
	dst := bytes.NewBufferString("untouched")
	if err = tag.Save(dst); err == nil || dst.String() != "untouched" {
		t.Fatalf("dest=%q err=%v", dst.String(), err)
	}
	if _, err := ReadMP4(bytes.NewReader(nil)); err == nil {
		t.Fatal("empty input accepted")
	}
}
