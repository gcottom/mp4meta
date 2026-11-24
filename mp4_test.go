package mp4meta

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"testing"

	mp4lib "github.com/abema/go-mp4"
	"github.com/aler9/writerseeker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// mockMP4Writer is a mock implementation of mp4Writer
type mockMP4Writer struct {
	mock.Mock
}

func (m *mockMP4Writer) Write(p []byte) (int, error) {
	w := &writerseeker.WriterSeeker{}
	b, _ := w.Write(p)
	args := m.Called(p)
	return b, args.Error(1)
}

func (m *mockMP4Writer) Seek(offset int64, whence int) (int64, error) {
	args := m.Called(offset, whence)
	return args.Get(0).(int64), args.Error(1)
}

func (m *mockMP4Writer) StartBox(bi *mp4lib.BoxInfo) (*mp4lib.BoxInfo, error) {
	args := m.Called(bi)
	return args.Get(0).(*mp4lib.BoxInfo), args.Error(1)
}

func (m *mockMP4Writer) EndBox() (*mp4lib.BoxInfo, error) {
	args := m.Called()
	return args.Get(0).(*mp4lib.BoxInfo), args.Error(1)
}

func (m *mockMP4Writer) CopyBox(r io.ReadSeeker, bi *mp4lib.BoxInfo) error {
	args := m.Called(r, bi)
	return args.Error(0)
}

func compareImages(src1 [][][3]float32, src2 [][][3]float32) bool {
	dif := 0
	for i, dat1 := range src1 {
		for j := range dat1 {
			if len(src1[i][j]) != len(src2[i][j]) {
				dif++
			}
		}
	}
	return dif == 0
}

func image_2_array_at(src image.Image) [][][3]float32 {
	bounds := src.Bounds()
	width, height := bounds.Max.X, bounds.Max.Y
	iaa := make([][][3]float32, height)

	for y := 0; y < height; y++ {
		row := make([][3]float32, width)
		for x := 0; x < width; x++ {
			r, g, b, _ := src.At(x, y).RGBA()
			// A color's RGBA method returns values in the range [0, 65535].
			// Shifting by 8 reduces this to the range [0, 255].
			row[x] = [3]float32{float32(r >> 8), float32(g >> 8), float32(b >> 8)}
		}
		iaa[y] = row
	}

	return iaa
}

func TestReadM4ATags(t *testing.T) {
	path, _ := filepath.Abs("./testdata/testdata-m4a.m4a")
	f, err := os.Open(path)
	assert.NoError(t, err)
	tag, err := ReadMP4(f)
	assert.NoError(t, err)
	assert.NotEmpty(t, tag.GetArtist())
	assert.NotEmpty(t, tag.GetAlbum())
	assert.NotEmpty(t, tag.GetTitle())
}

func TestM4A(t *testing.T) {
	t.Run("TestWriteEmptyTagsM4A-buffers", func(t *testing.T) {
		path, _ := filepath.Abs("./testdata/testdata-m4a-nonEmpty.m4a")
		f, err := os.Open(path)
		assert.NoError(t, err)
		b, err := io.ReadAll(f)
		assert.NoError(t, err)
		r := bytes.NewReader(b)
		tag, err := ReadMP4(r)
		assert.NoError(t, err)
		tag.ClearAllTags()
		buffy := new(bytes.Buffer)
		err = tag.Save(buffy)
		assert.NoError(t, err)
		r = bytes.NewReader(buffy.Bytes())
		tag, err = ReadMP4(r)
		assert.NoError(t, err)
		assert.Empty(t, tag.GetArtist())
		assert.Empty(t, tag.GetAlbum())
		assert.Empty(t, tag.GetTitle())
	})

	t.Run("TestWriteEmptyTagsM4A-file", func(t *testing.T) {
		dir := t.TempDir()
		of, err := os.ReadFile("./testdata/testdata-m4a-nonEmpty.m4a")
		assert.NoError(t, err)
		err = os.WriteFile(filepath.Join(dir, "testdata-m4a-nonEmpty.m4a"), of, 0755)
		assert.NoError(t, err)
		path, _ := filepath.Abs(filepath.Join(dir, "testdata-m4a-nonEmpty.m4a"))
		f, err := os.OpenFile(path, os.O_RDONLY, 0755)
		assert.NoError(t, err)
		defer f.Close()
		tag, err := ReadMP4(f)
		assert.NoError(t, err)
		tag.ClearAllTags()
		err = tag.Save(f)
		assert.NoError(t, err)
		_, err = f.Seek(0, io.SeekStart)
		assert.NoError(t, err)
		tag, err = ReadMP4(f)
		assert.NoError(t, err)
		f.Close()
		assert.Empty(t, tag.GetArtist())
		assert.Empty(t, tag.GetAlbum())
		assert.Empty(t, tag.GetTitle())
		assert.Equal(t, tag.GetYear(), 0)

	})

	t.Run("TestWriteTagsM4AFromEmpty-buffers", func(t *testing.T) {
		path, _ := filepath.Abs("./testdata/testdata-m4a-nonEmpty.m4a")
		f, err := os.Open(path)
		assert.NoError(t, err)
		defer f.Close()
		b, err := io.ReadAll(f)
		assert.NoError(t, err)
		r := bytes.NewReader(b)

		tag, err := ReadMP4(r)
		assert.NoError(t, err)
		tag.ClearAllTags()

		buffy := new(bytes.Buffer)
		err = tag.Save(buffy)
		assert.NoError(t, err)
		r = bytes.NewReader(buffy.Bytes())
		tag, err = ReadMP4(r)
		assert.NoError(t, err)
		tag.SetArtist("TestArtist1")
		tag.SetTitle("TestTitle1")
		tag.SetAlbum("TestAlbum1")
		tag.SetBPM(127)
		tag.SetTrackNumber(3)
		tag.SetTrackTotal(12)
		p, err := filepath.Abs("./testdata/testdata-img-1.jpg")
		assert.NoError(t, err)
		jp, err := os.Open(p)
		assert.NoError(t, err)
		j, err := jpeg.Decode(jp)
		assert.NoError(t, err)
		tag.SetCoverArt(&j)
		assert.NoError(t, err)

		buffy = new(bytes.Buffer)
		err = tag.Save(buffy)
		assert.NoError(t, err)
		r = bytes.NewReader(buffy.Bytes())
		tag, err = ReadMP4(r)
		assert.NoError(t, err)
		assert.Equal(t, tag.GetArtist(), "TestArtist1")
		assert.Equal(t, tag.GetAlbum(), "TestAlbum1")
		assert.Equal(t, tag.GetTitle(), "TestTitle1")
		assert.Equal(t, tag.GetBPM(), 127)
		assert.Equal(t, tag.GetTrackNumber(), 3)
		assert.Equal(t, tag.GetTrackTotal(), 12)

		img1data := image_2_array_at(j)
		img2data := image_2_array_at(*tag.CoverArt)

		assert.True(t, compareImages(img1data, img2data))

	})

	t.Run("TestWriteTagsM4AFromEmpty-file", func(t *testing.T) {
		dir := t.TempDir()
		of, err := os.ReadFile("./testdata/testdata-m4a-nonEmpty.m4a")
		assert.NoError(t, err)
		err = os.WriteFile(filepath.Join(dir, "testdata-m4a-nonEmpty.m4a"), of, 0755)
		assert.NoError(t, err)
		path, _ := filepath.Abs(filepath.Join(dir, "testdata-m4a-nonEmpty.m4a"))
		f, err := os.Open(path)
		assert.NoError(t, err)
		defer f.Close()

		tag, err := ReadMP4(f)
		assert.NoError(t, err)
		tag.SetArtist("TestArtist1")
		tag.SetTitle("TestTitle1")
		tag.SetAlbum("TestAlbum1")
		p, err := filepath.Abs("./testdata/testdata-img-1.jpg")
		assert.NoError(t, err)
		jp, err := os.Open(p)
		assert.NoError(t, err)
		j, err := jpeg.Decode(jp)
		assert.NoError(t, err)
		tag.SetCoverArt(&j)
		assert.NoError(t, err)

		err = tag.Save(f)
		assert.NoError(t, err)

		_, err = f.Seek(0, io.SeekStart)
		assert.NoError(t, err)
		tag, err = ReadMP4(f)
		assert.NoError(t, err)
		assert.Equal(t, tag.GetArtist(), "TestArtist1")
		assert.Equal(t, tag.GetAlbum(), "TestAlbum1")
		assert.Equal(t, tag.GetTitle(), "TestTitle1")

		img1data := image_2_array_at(j)
		img2data := image_2_array_at(*tag.CoverArt)

		assert.True(t, compareImages(img1data, img2data))

	})

	t.Run("TestUpdateTagsM4A-buffers", func(t *testing.T) {
		path, _ := filepath.Abs("./testdata/testdata-m4a-nonEmpty.m4a")
		f, err := os.Open(path)
		assert.NoError(t, err)
		defer f.Close()
		b, err := io.ReadAll(f)
		assert.NoError(t, err)
		r := bytes.NewReader(b)

		tag, err := ReadMP4(r)
		assert.NoError(t, err)
		tag.ClearAllTags()

		buffy := new(bytes.Buffer)
		err = tag.Save(buffy)
		assert.NoError(t, err)
		r = bytes.NewReader(buffy.Bytes())
		tag, err = ReadMP4(r)
		assert.NoError(t, err)
		tag.SetArtist("TestArtist1")
		tag.SetTitle("TestTitle1")
		tag.SetAlbum("TestAlbum1")
		p, err := filepath.Abs("./testdata/testdata-img-1.jpg")
		assert.NoError(t, err)
		jp, err := os.Open(p)
		assert.NoError(t, err)
		j, err := jpeg.Decode(jp)
		assert.NoError(t, err)
		tag.SetCoverArt(&j)
		assert.NoError(t, err)

		buffy = new(bytes.Buffer)
		err = tag.Save(buffy)
		assert.NoError(t, err)
		r = bytes.NewReader(buffy.Bytes())
		tag, err = ReadMP4(r)
		assert.NoError(t, err)
		assert.Equal(t, tag.GetArtist(), "TestArtist1")
		assert.Equal(t, tag.GetAlbum(), "TestAlbum1")
		assert.Equal(t, tag.GetTitle(), "TestTitle1")

		tag.SetArtist("TestArtist2")

		buffy = new(bytes.Buffer)
		err = tag.Save(buffy)
		assert.NoError(t, err)

		r = bytes.NewReader(buffy.Bytes())
		tag, err = ReadMP4(r)
		assert.NoError(t, err)
		assert.Equal(t, tag.GetArtist(), "TestArtist2")
		assert.Equal(t, tag.GetAlbum(), "TestAlbum1")
		assert.Equal(t, tag.GetTitle(), "TestTitle1")

		img1data := image_2_array_at(j)
		img2data := image_2_array_at(*tag.CoverArt)

		assert.True(t, compareImages(img1data, img2data))

	})

	t.Run("TestUpdateTagsM4A-file", func(t *testing.T) {
		dir := t.TempDir()
		of, err := os.ReadFile("./testdata/testdata-m4a-nonEmpty.m4a")
		assert.NoError(t, err)
		err = os.WriteFile(filepath.Join(dir, "testdata-m4a-nonEmpty.m4a"), of, 0755)
		assert.NoError(t, err)
		path, _ := filepath.Abs(filepath.Join(dir, "testdata-m4a-nonEmpty.m4a"))
		f, err := os.Open(path)
		assert.NoError(t, err)
		defer f.Close()

		tag, err := ReadMP4(f)
		assert.NoError(t, err)
		tag.SetArtist("TestArtist1")
		tag.SetTitle("TestTitle1")
		tag.SetAlbum("TestAlbum1")
		p, err := filepath.Abs("./testdata/testdata-img-1.jpg")
		assert.NoError(t, err)
		jp, err := os.Open(p)
		assert.NoError(t, err)
		j, err := jpeg.Decode(jp)
		assert.NoError(t, err)
		tag.SetCoverArt(&j)
		assert.NoError(t, err)
		err = tag.Save(f)
		assert.NoError(t, err)

		_, err = f.Seek(0, io.SeekStart)
		assert.NoError(t, err)
		tag, err = ReadMP4(f)
		assert.NoError(t, err)
		assert.Equal(t, tag.GetArtist(), "TestArtist1")
		assert.Equal(t, tag.GetAlbum(), "TestAlbum1")
		assert.Equal(t, tag.GetTitle(), "TestTitle1")

		tag.SetArtist("TestArtist2")
		err = tag.Save(f)
		assert.NoError(t, err)

		_, err = f.Seek(0, io.SeekStart)
		assert.NoError(t, err)
		tag, err = ReadMP4(f)
		assert.NoError(t, err)
		f.Close()
		assert.Equal(t, tag.GetArtist(), "TestArtist2")
		assert.Equal(t, tag.GetAlbum(), "TestAlbum1")
		assert.Equal(t, tag.GetTitle(), "TestTitle1")

		img1data := image_2_array_at(j)
		img2data := image_2_array_at(*tag.CoverArt)

		assert.True(t, compareImages(img1data, img2data))

	})
	t.Run("TestNoChangeM4A-file", func(t *testing.T) {
		dir := t.TempDir()
		of, err := os.ReadFile("./testdata/test1.m4a")
		assert.NoError(t, err)
		err = os.WriteFile(filepath.Join(dir, "test1.m4a"), of, 0755)
		assert.NoError(t, err)
		path, _ := filepath.Abs(filepath.Join(dir, "test1.m4a"))
		f, err := os.Open(path)
		assert.NoError(t, err)
		defer f.Close()

		tag, err := ReadMP4(f)
		assert.NoError(t, err)
		err = tag.Save(f)
		assert.NoError(t, err)

		_, err = f.Seek(0, io.SeekStart)
		assert.NoError(t, err)
		tag, err = ReadMP4(f)
		assert.NoError(t, err)
		assert.Equal(t, tag.GetArtist(), "test1")
		assert.Equal(t, tag.GetAlbum(), "test1")
		assert.Equal(t, tag.GetTitle(), "test1")
	})

	t.Run("TestWriteAllTagsM4AFromEmpty-file", func(t *testing.T) {
		dir := t.TempDir()
		of, err := os.ReadFile("./testdata/testdata-m4a-nonEmpty.m4a")
		assert.NoError(t, err)
		err = os.WriteFile(filepath.Join(dir, "testdata-m4a-nonEmpty.m4a"), of, 0755)
		assert.NoError(t, err)
		path, _ := filepath.Abs(filepath.Join(dir, "testdata-m4a-nonEmpty.m4a"))
		f, err := os.Open(path)
		assert.NoError(t, err)
		defer f.Close()

		tag, err := ReadMP4(f)
		assert.NoError(t, err)
		tag.SetArtist("TestArtist1")
		tag.SetTitle("TestTitle1")
		tag.SetAlbum("TestAlbum1")
		tag.SetAlbumArtist("AlbumArtist1")
		tag.SetComments("A comment about comments")
		tag.SetComposer("someone composed")
		tag.SetCopyright("please don't steal me")
		tag.SetEncoder("encoder da ba dee 23")
		tag.SetGenre("Metalcore")
		tag.SetTrackNumber(3)
		tag.SetTrackTotal(12)
		tag.SetDiscNumber(1)
		tag.SetDiscTotal(3)
		tag.SetYear(2077)
		p, err := filepath.Abs("./testdata/testdata-img-1.jpg")
		assert.NoError(t, err)
		jp, err := os.Open(p)
		assert.NoError(t, err)
		j, err := jpeg.Decode(jp)
		assert.NoError(t, err)
		tag.SetCoverArt(&j)
		assert.NoError(t, err)

		err = tag.Save(f)
		assert.NoError(t, err)

		_, err = f.Seek(0, io.SeekStart)
		assert.NoError(t, err)
		tag, err = ReadMP4(f)
		assert.NoError(t, err)
		assert.Equal(t, tag.GetArtist(), "TestArtist1")
		assert.Equal(t, tag.GetAlbum(), "TestAlbum1")
		assert.Equal(t, tag.GetTitle(), "TestTitle1")
		assert.Equal(t, tag.GetAlbumArtist(), "AlbumArtist1")
		assert.Equal(t, tag.GetComments(), "A comment about comments")
		assert.Equal(t, tag.GetComposer(), "someone composed")
		assert.Equal(t, tag.GetCopyright(), "please don't steal me")
		assert.Equal(t, tag.GetEncoder(), "encoder da ba dee 23")
		assert.Equal(t, tag.GetGenre(), "Metalcore")
		assert.Equal(t, tag.GetTrackNumber(), 3)
		assert.Equal(t, tag.GetTrackTotal(), 12)
		assert.Equal(t, tag.GetDiscNumber(), 1)
		assert.Equal(t, tag.GetDiscTotal(), 3)
		assert.Equal(t, tag.GetYear(), 2077)
		img1data := image_2_array_at(j)
		img2data := image_2_array_at(*tag.GetCoverArt())

		assert.True(t, compareImages(img1data, img2data))

	})
}

func TestSaveMP4WriterErrors(t *testing.T) {
	dir := t.TempDir()
	of, err := os.ReadFile("./testdata/testdata-m4a-nonEmpty.m4a")
	assert.NoError(t, err)
	err = os.WriteFile(filepath.Join(dir, "testdata-m4a-nonEmpty.m4a"), of, 0755)
	assert.NoError(t, err)
	path, _ := filepath.Abs(filepath.Join(dir, "testdata-m4a-nonEmpty.m4a"))
	f, err := os.Open(path)
	assert.NoError(t, err)
	defer f.Close()

	t.Run("copy box error", func(t *testing.T) {
		mp4WriterMock := new(mockMP4Writer)
		//mp4WriterMock.On("StartBox", mock.Anything).Return(&mp4lib.BoxInfo{}, errors.New("error starting box"))
		mp4WriterMock.On("CopyBox", mock.Anything, mock.Anything).Return(errors.New("error copying box"))
		buf := new(bytes.Buffer)
		tag, err := ReadMP4(f)
		assert.NoError(t, err)
		tag.SetArtist("TestArtist1")
		err = saveMP4(tag.reader, buf, mp4WriterMock, &writerseeker.WriterSeeker{}, tag)
		assert.EqualError(t, err, "error copying box")
	})
	t.Run("moov box start error", func(t *testing.T) {
		mp4WriterMock := new(mockMP4Writer)
		mp4WriterMock.On("StartBox", mock.Anything).Return(&mp4lib.BoxInfo{}, errors.New("error starting box"))
		mp4WriterMock.On("CopyBox", mock.Anything, mock.Anything).Return(nil)
		buf := new(bytes.Buffer)
		tag, err := ReadMP4(f)
		assert.NoError(t, err)
		tag.SetArtist("TestArtist1")
		err = saveMP4(tag.reader, buf, mp4WriterMock, &writerseeker.WriterSeeker{}, tag)
		assert.EqualError(t, err, "error starting box")
	})
	t.Run("udta box start error", func(t *testing.T) {
		mp4WriterMock := new(mockMP4Writer)
		mp4WriterMock.On("StartBox", mock.Anything).Return(&mp4lib.BoxInfo{}, nil).Once()
		mp4WriterMock.On("StartBox", mock.Anything).Return(&mp4lib.BoxInfo{}, errors.New("error starting box"))
		mp4WriterMock.On("CopyBox", mock.Anything, mock.Anything).Return(nil)
		buf := new(bytes.Buffer)
		tag, err := ReadMP4(f)
		assert.NoError(t, err)
		tag.SetArtist("TestArtist1")
		err = saveMP4(tag.reader, buf, mp4WriterMock, &writerseeker.WriterSeeker{}, tag)
		assert.EqualError(t, err, "error starting box")
	})
	t.Run("meta box start error", func(t *testing.T) {
		mp4WriterMock := new(mockMP4Writer)
		mp4WriterMock.On("StartBox", mock.Anything).Return(&mp4lib.BoxInfo{}, nil).Twice()
		mp4WriterMock.On("StartBox", mock.Anything).Return(&mp4lib.BoxInfo{}, errors.New("error starting box"))
		mp4WriterMock.On("CopyBox", mock.Anything, mock.Anything).Return(nil)
		buf := new(bytes.Buffer)
		tag, err := ReadMP4(f)
		assert.NoError(t, err)
		tag.SetArtist("TestArtist1")
		err = saveMP4(tag.reader, buf, mp4WriterMock, &writerseeker.WriterSeeker{}, tag)
		assert.EqualError(t, err, "error starting box")
	})
}
