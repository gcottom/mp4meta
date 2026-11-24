package mp4meta

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
	"io"
	"math/rand"
	"os"
	"reflect"
	"strings"
	"testing"

	core "github.com/gcottom/audiometa-core/v2"
	"github.com/gcottom/audiometa-core/v2/contracttest"
)

var _ map[MP4Tag]struct{}

func TestCanonicalMP4PreservesItems(t *testing.T) {
	foreign, err := newItem("----:example.test:UNRECOGNIZED", []metadataCell{{99, []byte{0, 42, 255}}})
	if err != nil {
		t.Fatal(err)
	}
	trim, err := newItem(itunesTrim, []metadataCell{{mp4Text, []byte(" 00000000 00000840 00000011 00000000000C4000")}})
	if err != nil {
		t.Fatal(err)
	}
	language := makeBox("\251nam", makeBox("data", []byte{0, 0, 0, 1, 0, 0, 0, 1}, []byte("ancienne")))
	title := makeBox("\251nam", makeBox("data", []byte{0, 0, 0, 1, 0, 0, 0, 0}, []byte("Older")))
	legacy := makeBox("gnre", makeBox("data", []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 18}))
	fallback, err := newItem(fallbackField(core.Genre), []metadataCell{{mp4Text, []byte("Conflicting value")}})
	if err != nil {
		t.Fatal(err)
	}
	unknownSibling := makeBox("xyzz", []byte("outside the item list"))
	src := simpleMP4(makeBox("udta", iTunesMetadata(unknownSibling,
		makeBox("ilst", foreign, language, title, legacy, fallback, trim))))
	editor := openCanonical(t, src)
	assertField(t, editor, core.Title, core.TextValue("ancienne"))
	assertField(t, editor, core.Genre, core.TextValue("Rock"))
	assertField(t, editor, core.EncoderDelay, core.IntValue(2112))
	assertField(t, editor, core.EncoderPadding, core.IntValue(17))

	if actual := renderCanonical(t, editor); !bytes.Equal(actual, src) {
		t.Fatal("unmodified save changed bytes")
	}
	must(t, editor.SetField(core.Genre, core.TextValue("Ambient"), core.TextValue("Jazz")))
	must(t, editor.SetField(core.TrackTotal, core.IntValue(0)))
	must(t, editor.SetField(core.TrackNumber, core.IntValue(65535)))
	must(t, editor.SetField(core.Artist, core.TextValue(""), core.TextValue("Ket\u0105")))
	must(t, editor.SetField(core.Compilation, core.BoolValue(false)))
	must(t, editor.SetCustom("COLLECTION", "", "\u5317\u4eac", "value=contains=equals"))
	first := renderCanonical(t, editor)
	second := renderCanonical(t, editor)
	if !bytes.Equal(first, second) {
		t.Fatal("repeated save was non-deterministic")
	}
	for _, expected := range [][]byte{foreign, trim, language, title, legacy, fallback, unknownSibling} {
		if !bytes.Contains(first, expected) {
			t.Fatalf("unrelated or conflicting item lost: %x", expected)
		}
	}
	after := openCanonical(t, first)
	assertField(t, after, core.Genre, core.TextValue("Ambient"), core.TextValue("Jazz"))
	assertField(t, after, core.TrackTotal, core.IntValue(0))
	assertField(t, after, core.TrackNumber, core.IntValue(65535))
	assertField(t, after, core.Artist, core.TextValue(""), core.TextValue("Ket\u0105"))
	assertField(t, after, core.Compilation, core.BoolValue(false))
	if actual, ok := after.GetCustom("collection"); !ok || !reflect.DeepEqual(actual, []string{"", "\u5317\u4eac", "value=contains=equals"}) {
		t.Fatalf("custom field = %q, %v", actual, ok)
	}
	must(t, after.RemoveField(core.TrackNumber))
	must(t, after.RemoveField(core.Genre))
	must(t, after.RemoveCustomValue("collection", "\u5317\u4eac"))
	without := openCanonical(t, renderCanonical(t, after))
	assertField(t, without, core.TrackNumber)
	assertField(t, without, core.TrackTotal, core.IntValue(0))
	assertField(t, without, core.Genre)
	if actual, ok := without.GetCustom("COLLECTION"); !ok || !reflect.DeepEqual(actual, []string{"", "value=contains=equals"}) {
		t.Fatalf("custom field after removal = %q, %v", actual, ok)
	}
	without.ClearAll(true)
	preserved := renderCanonical(t, without)
	if !bytes.Contains(preserved, foreign) || !bytes.Contains(preserved, trim) {
		t.Fatal("clear with preservation removed an unknown or technical item")
	}
	without.ClearAll(false)
	cleared := renderCanonical(t, without)
	if bytes.Contains(cleared, foreign) || !bytes.Contains(cleared, trim) || !bytes.Contains(cleared, unknownSibling) {
		t.Fatal("clear changed data outside descriptive items or kept an unknown item")
	}
}

func TestCanonicalMP4FastStartGrowth(t *testing.T) {
	src, err := os.ReadFile("testdata/faststart-sine.m4a")
	must(t, err)
	before, err := inspectMP4(bytes.NewReader(src))
	must(t, err)
	editor := openCanonical(t, src)
	assertField(t, editor, core.Title, core.TextValue("Before"))
	must(t, editor.SetField(core.Title, core.TextValue(strings.Repeat("\u984c\u540d ", 2048))))
	output := renderCanonical(t, editor)
	after, err := inspectMP4(bytes.NewReader(output))
	must(t, err)
	if before.media[0].start >= after.media[0].start ||
		!bytes.Equal(src[before.media[0].payload():before.media[0].end], output[after.media[0].payload():after.media[0].end]) {
		t.Fatal("media payload failed preservation during fast-start relocation")
	}
	i := bytes.Index(output, []byte("stco"))
	if i < 0 || int64(binary.BigEndian.Uint32(output[i+12:])) != after.media[0].payload() {
		t.Fatal("output chunk offset does not point at the preserved media payload")
	}
	if err := editor.Save(shortWriter{}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write returned %v", err)
	}
	assertField(t, openCanonical(t, output), core.Title, core.TextValue(strings.Repeat("\u984c\u540d ", 2048)))
}

func TestCanonicalMP4ArtworkAndChapters(t *testing.T) {
	jpegBytes, err := os.ReadFile("testdata/testdata-img-1.jpg")
	must(t, err)
	source := simpleMP4(nil)
	editor := openCanonical(t, source)
	native := core.PictureValue(core.Picture{MIME: "image/jpeg", Data: jpegBytes})
	extension := core.PictureValue(core.Picture{MIME: "image/jpeg", Role: 4, Description: "Back", Width: 123, Data: jpegBytes})
	chapter := core.ChapterValue(core.Chapter{ID: "chpl-1", Title: "First", StartMS: 2000})
	must(t, editor.SetField(core.Artwork, native))
	must(t, editor.SetField(core.Chapters, chapter))
	tagged := renderCanonical(t, editor)
	if !bytes.Contains(tagged, jpegBytes) || !bytes.Contains(tagged, []byte("covr")) || !bytes.Contains(tagged, []byte("chpl")) {
		t.Fatal("native artwork and chapter carriers missing")
	}
	editor = openCanonical(t, tagged)
	assertField(t, editor, core.Artwork, native)
	assertField(t, editor, core.Chapters, chapter)
	must(t, editor.SetField(core.Title, core.TextValue("Title only")))
	if data := renderCanonical(t, editor); !bytes.Contains(data, jpegBytes) || !bytes.Contains(data, []byte("chpl")) {
		t.Fatal("unmodified encoded artwork and chapters did not survive a title edit")
	}
	must(t, editor.SetField(core.Artwork, native, extension))
	exactChapter := core.ChapterValue(core.Chapter{ID: "intro", Title: "First", StartMS: 2000, EndMS: 4000})
	must(t, editor.SetField(core.Chapters, exactChapter))
	extended := openCanonical(t, renderCanonical(t, editor))
	assertField(t, extended, core.Artwork, native, extension)
	assertField(t, extended, core.Chapters, exactChapter)
}

func TestCanonicalMP4PreservesNonITunesCollections(t *testing.T) {
	mdtaHandler := make([]byte, 25)
	copy(mdtaHandler[8:], "mdta")
	keyed := makeBox("meta", make([]byte, 4), makeBox("hdlr", mdtaHandler),
		makeBox("keys", []byte("opaque-key-table")), makeBox("ilst", []byte("opaque-keyed-values")))
	perTrack := makeBox("trak", makeBox("udta", keyed))
	source := simpleMP4(bytes.Join([][]byte{keyed, perTrack, makeBox("udta", iTunesMetadata(makeBox("ilst")))}, nil))
	editor := openCanonical(t, source)
	must(t, editor.SetField(core.Title, core.TextValue("Movie scoped")))
	tagged := renderCanonical(t, editor)
	if !bytes.Contains(tagged, keyed) || !bytes.Contains(tagged, perTrack) {
		t.Fatal("a keyed movie or track collection was altered")
	}
	assertField(t, openCanonical(t, tagged), core.Title, core.TextValue("Movie scoped"))
}

func TestCanonicalMP4UnsafeLayoutsFailBeforeOutput(t *testing.T) {
	qtChapters := makeBox("trak", makeBox("tref", makeBox("chap", encodedUint32(1))))
	input := simpleMP4(qtChapters)
	editor := openCanonical(t, input)
	must(t, editor.SetField(core.Chapters, core.ChapterValue(core.Chapter{ID: "new", Title: "New"})))
	out := bytes.NewBufferString("unchanged")
	if err := editor.Save(out); !errors.Is(err, core.ErrUnsupported) || out.String() != "unchanged" {
		t.Fatalf("unsafe chapter edit result %v, output = %q", err, out.String())
	}
	input = append(input, makeBox("moof")...)
	editor = openCanonical(t, input)
	must(t, editor.SetField(core.Title, core.TextValue("Cannot edit")))
	if err := editor.Save(out); !errors.Is(err, core.ErrUnsupported) || out.String() != "unchanged" {
		t.Fatalf("fragment edit result %v, output = %q", err, out.String())
	}
}

func FuzzCanonicalMP4(f *testing.F) {
	src, err := os.ReadFile("testdata/faststart-sine.m4a")
	must(f, err)
	f.Add(src)
	f.Add(simpleMP4(makeBox("udta", iTunesMetadata(makeBox("ilst")))))
	f.Fuzz(func(t *testing.T, data []byte) {
		editor, err := OpenMetadata(bytes.NewReader(data))
		if err != nil {
			return
		}
		copy := renderCanonical(t, editor)
		if !bytes.Equal(copy, data) {
			t.Fatal("unmodified round trip changed data")
		}
		_ = editor.SetField(core.Title, core.TextValue("fuzz title"))
		_ = editor.Save(io.Discard)
	})
}

func BenchmarkCanonicalMP4Save(b *testing.B) {
	title := core.TextValue("new title")
	for _, size := range []int{1024, 4 << 20, 32 << 20} {
		src := bytes.Join([][]byte{makeBox("mdat", make([]byte, size)), makeBox("moov")}, nil)
		b.Run(benchmarkName(size), func(b *testing.B) {
			editor := openCanonical(b, src)
			must(b, editor.SetField(core.Title, title))
			b.SetBytes(int64(len(src)))
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				must(b, editor.Save(io.Discard))
			}
		})
	}
}

func BenchmarkCanonicalMP4Artwork(b *testing.B) {
	src, err := os.ReadFile("testdata/testdata-m4a.m4a")
	must(b, err)
	pixels := image.NewNRGBA(image.Rect(0, 0, 1024, 1024))
	pseudorandom := rand.New(rand.NewSource(1))
	_, err = pseudorandom.Read(pixels.Pix)
	must(b, err)
	var encoded bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.NoCompression}
	must(b, encoder.Encode(&encoded, pixels))
	for _, role := range []struct {
		name string
		id   uint32
	}{{"native", 0}, {"rich", 3}} {
		b.Run(role.name, func(b *testing.B) {
			editor := openCanonical(b, src)
			picture := core.PictureValue(core.Picture{Role: role.id, MIME: "image/png", Data: encoded.Bytes()})
			must(b, editor.SetField(core.Artwork, picture))
			b.SetBytes(int64(encoded.Len()))
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				must(b, editor.Save(io.Discard))
			}
		})
	}
}

func benchmarkName(n int) string {
	switch n {
	case 1024:
		return "1KiB"
	case 4 << 20:
		return "4MiB"
	default:
		return "32MiB"
	}
}

func openCanonical(t testing.TB, src []byte) *core.Editor {
	t.Helper()
	editor, err := OpenMetadata(bytes.NewReader(src))
	must(t, err)
	return editor
}

func renderCanonical(t testing.TB, e *core.Editor) []byte {
	t.Helper()
	var out bytes.Buffer
	must(t, e.Save(&out))
	return out.Bytes()
}

func assertField(t testing.TB, e *core.Editor, field core.Field, expected ...core.Value) {
	t.Helper()
	values, found := e.Get(field)
	if found != (len(expected) != 0) || !reflect.DeepEqual(values, expected) {
		t.Fatalf("%s loaded %v (%v), want %v", field, values, found, expected)
	}
}

func must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func iTunesMetadata(children ...[]byte) []byte {
	handler := make([]byte, 25)
	copy(handler[8:], "mdirappl")
	return makeBox("meta", make([]byte, 4), makeBox("hdlr", handler), bytes.Join(children, nil))
}

func simpleMP4(moov []byte) []byte {
	return bytes.Join([][]byte{makeBox("ftyp", []byte("M4A \x00\x00\x00\x00")), makeBox("mdat", []byte{1, 2, 3}), makeBox("moov", moov)}, nil)
}

func TestCanonicalMP4SharedFieldContract(t *testing.T) {
	fixture, err := os.ReadFile("testdata/faststart-sine.m4a")
	must(t, err)
	contracttest.Run(t, OpenMetadata, fixture)
}

func TestLegacyMP4PreservesCanonicalAndArtwork(t *testing.T) {
	src, err := os.ReadFile("testdata/faststart-sine.m4a")
	must(t, err)
	jpeg, err := os.ReadFile("testdata/testdata-img-1.jpg")
	must(t, err)
	e := openCanonical(t, src)
	art := core.PictureValue(core.Picture{MIME: "image/jpeg", Data: jpeg})
	must(t, e.SetField(core.Artwork, art))
	must(t, e.SetField(core.Artist, core.TextValue("One"), core.TextValue("Two")))
	must(t, e.SetCustom("COLLECTION", "keep me"))
	extended := renderCanonical(t, e)
	legacy, err := ReadMP4(bytes.NewReader(extended))
	must(t, err)
	legacy.Title = "Legacy direct field edit"
	legacy.SetBPM(123)
	var output bytes.Buffer
	must(t, legacy.Save(&output))
	got := openCanonical(t, output.Bytes())
	assertField(t, got, core.Title, core.TextValue("Legacy direct field edit"))
	assertField(t, got, core.Artwork, art)
	assertField(t, got, core.Artist, core.TextValue("One"), core.TextValue("Two"))
	assertField(t, got, core.BPM, core.IntValue(123))
	if values, ok := got.GetCustom("COLLECTION"); !ok || len(values) != 1 || values[0] != "keep me" {
		t.Fatal("legacy save removed unmodified custom metadata")
	}
	legacy.ClearAllTags()
	output.Reset()
	must(t, legacy.Save(&output))
	got = openCanonical(t, output.Bytes())
	assertField(t, got, core.Artwork)
	assertField(t, got, core.Title)
	if _, ok := got.GetCustom("COLLECTION"); !ok {
		t.Fatal("legacy clear removed metadata outside its fields")
	}
}

func TestCanonicalAndLegacyReadHistoricalTempo(t *testing.T) {
	item := makeBox("tmpo", makeBox("data", []byte{0, 0, 0, 21, 0, 0, 0, 0, 0, 120, 0, 0}))
	src := simpleMP4(makeBox("udta", iTunesMetadata(makeBox("ilst", item))))
	assertField(t, openCanonical(t, src), core.BPM, core.IntValue(120))
	legacy, err := ReadMP4(bytes.NewReader(src))
	must(t, err)
	if legacy.GetBPM() != 120 {
		t.Fatalf("legacy tempo = %d", legacy.GetBPM())
	}
}

func TestCanonicalMP4Co64Preflight(t *testing.T) {
	media := []byte{1, 2, 3, 4, 5}
	build := func(offset uint64) []byte {
		offsets := makeBox("co64", make([]byte, 4), encodedUint32(1), encodedUint64(offset))
		movie := makeBox("moov", makeBox("trak", makeBox("mdia", makeBox("minf", makeBox("stbl", offsets)))))
		return append(movie, makeBox("mdat", media)...)
	}
	old := build(uint64(len(build(0)) - len(media)))
	editor := openCanonical(t, old)
	must(t, editor.SetField(core.Title, core.TextValue("larger movie metadata")))
	output := renderCanonical(t, editor)
	i := bytes.Index(output, []byte("co64"))
	if i < 0 {
		t.Fatal("co64 table missing")
	}
	chunk := binary.BigEndian.Uint64(output[i+12:])
	if chunk <= uint64(len(old)-len(media)) || chunk > uint64(len(output)) || !bytes.Equal(output[chunk:], media) {
		t.Fatal("co64 table does not point at relocated media")
	}

	editor = openCanonical(t, build(1))
	must(t, editor.SetField(core.Title, core.TextValue("new movie metadata")))
	dst := bytes.NewBufferString("untouched")
	if err := editor.Save(dst); err == nil || dst.String() != "untouched" {
		t.Fatalf("invalid offset table: output %q, error %v", dst.String(), err)
	}
}

func TestCanonicalMP4RejectsUnsafeIndexes(t *testing.T) {
	source := simpleMP4(makeBox("udta", iTunesMetadata(makeBox("ilst"), makeBox("iloc", []byte{0}))))
	editor := openCanonical(t, source)
	must(t, editor.SetField(core.Title, core.TextValue("new title")))
	output := bytes.NewBufferString("untouched")
	if err := editor.Save(output); !errors.Is(err, core.ErrUnsupported) || output.String() != "untouched" {
		t.Fatalf("unsafe index save: output %q, error %v", output.String(), err)
	}
}

func TestCanonicalMP4TempoDoesNotCollideWithHistoricalPadding(t *testing.T) {
	editor := openCanonical(t, simpleMP4(nil))
	must(t, editor.SetField(core.BPM, core.IntValue(65536)))
	assertField(t, openCanonical(t, renderCanonical(t, editor)), core.BPM, core.IntValue(65536))
}
