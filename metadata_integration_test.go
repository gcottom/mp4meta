package mp4meta

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	core "github.com/gcottom/audiometa-core/v2"
)

func TestCanonicalMP4IndependentInteroperability(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg is required for decoded audio verification")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("FFprobe is required for independent stream verification")
	}
	source := "testdata/faststart-sine.m4a"
	src, err := os.Open(source)
	must(t, err)
	defer src.Close()
	e, err := OpenMetadata(src)
	must(t, err)
	must(t, e.SetField(core.Title, core.TextValue("AudioMeta MP4 interop")))
	must(t, e.SetField(core.Artist, core.TextValue("AudioMeta"), core.TextValue("Example Artist")))
	must(t, e.SetField(core.TrackNumber, core.IntValue(7)))
	must(t, e.SetField(core.TrackTotal, core.IntValue(20)))
	must(t, e.SetField(core.BPM, core.IntValue(120)))
	must(t, e.SetField(core.Compilation, core.BoolValue(true)))
	must(t, e.SetField(core.ISRC, core.TextValue("USABC2600001")))
	must(t, e.SetField(core.Rating, core.IntValue(0)))
	must(t, e.SetField(core.Chapters, core.ChapterValue(core.Chapter{ID: "chpl-1", Title: "Introduction"})))
	must(t, e.SetCustom("COLLECTION", "Sample", "Another"))
	jpg, err := os.ReadFile("testdata/testdata-img-1.jpg")
	must(t, err)
	must(t, e.SetField(core.Artwork, core.PictureValue(core.Picture{MIME: "image/jpeg", Data: jpg})))

	path := filepath.Join(t.TempDir(), "updated.m4a")
	out, err := os.Create(path)
	must(t, err)
	if err := e.Save(out); err != nil {
		out.Close()
		t.Fatal(err)
	}
	must(t, out.Close())
	pcm := func(file string) []byte {
		return commandOutput(t, ffmpeg, "-v", "error", "-i", file, "-map", "0:a:0",
			"-c:a", "pcm_s32le", "-f", "s32le", "-")
	}
	originalPCM := pcm(source)
	if len(originalPCM) == 0 || !bytes.Equal(originalPCM, pcm(path)) {
		t.Fatal("FFmpeg decoded different PCM after the metadata edit")
	}
	stream := func(file string) []byte {
		return commandOutput(t, ffprobe, "-v", "error", "-select_streams", "a:0",
			"-show_entries", "stream=codec_name,codec_tag_string,profile,sample_rate,channels,channel_layout,time_base,start_pts,duration_ts,duration,extradata_size",
			"-of", "json", file)
	}
	if !bytes.Equal(stream(source), stream(path)) {
		t.Fatal("FFprobe audio stream configuration or duration changed")
	}
	chapters := commandOutput(t, ffprobe, "-v", "error", "-show_chapters", "-of", "json", path)
	if !bytes.Contains(chapters, []byte("Introduction")) {
		t.Fatalf("FFprobe did not read the Nero chapter: %s", chapters)
	}

	t.Run("Mutagen", func(t *testing.T) {
		python := "python3"
		if runtime.GOOS == "windows" {
			python = "py"
		}
		if err := exec.Command(python, "-c", "import mutagen").Run(); err != nil {
			t.Skip("Python and Mutagen are required for independent MP4 tag verification")
		}
		check := `
import sys
from pathlib import Path
from mutagen.mp4 import MP4, MP4Cover
tags = MP4(sys.argv[1]).tags
assert tags["\xa9nam"] == ["AudioMeta MP4 interop"], tags
assert tags["\xa9ART"] == ["AudioMeta", "Example Artist"], tags
assert tags["trkn"] == [(7, 20)], tags
assert tags["tmpo"] == [120], tags
assert tags["cpil"] is True, tags
assert tags["----:com.apple.iTunes:ISRC"] == [b"USABC2600001"], tags
assert tags["----:io.github.gcottom.audiometa:audiometa/1/rating"] == [b"0"], tags
assert tags["----:io.github.gcottom.audiometa:audiometa/1/custom/COLLECTION"] == [b"Sample", b"Another"], tags
assert bytes(tags["covr"][0]) == Path(sys.argv[2]).read_bytes(), tags
assert tags["covr"][0].imageformat == MP4Cover.FORMAT_JPEG, tags
`
		commandOutput(t, python, "-c", check, path, "testdata/testdata-img-1.jpg")
	})
}

func commandOutput(t *testing.T, exe string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(exe, args...)
	var diagnostic bytes.Buffer
	cmd.Stderr = &diagnostic
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(fmt.Errorf("%s: %w: %s", exe, err, diagnostic.String()))
	}
	return out
}

func TestCanonicalMP4SameFileSaveRejected(t *testing.T) {
	input := simpleMP4(nil)
	path := filepath.Join(t.TempDir(), "same.m4a")
	must(t, os.WriteFile(path, input, 0600))
	file, err := os.OpenFile(path, os.O_RDWR, 0600)
	must(t, err)
	defer file.Close()
	e, err := OpenMetadata(file)
	must(t, err)
	must(t, e.SetField(core.Title, core.TextValue("new title")))
	if err := e.Save(file); err == nil {
		t.Fatal("same file save succeeded unexpectedly")
	}
	_, err = file.Seek(0, io.SeekStart)
	must(t, err)
	actual, err := io.ReadAll(file)
	must(t, err)
	if !bytes.Equal(actual, input) {
		t.Fatal("source changed after failed same-file save")
	}
}
