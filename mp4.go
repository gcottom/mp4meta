package mp4meta

import (
	"fmt"
	"image"
	"io"
	"strconv"

	mp4lib "github.com/abema/go-mp4"
)

var atomsMap = map[mp4lib.BoxType]string{
	{'\251', 'a', 'l', 'b'}: "Album",
	{'a', 'A', 'R', 'T'}:    "AlbumArtist",
	{'\251', 'A', 'R', 'T'}: "Artist",
	{'\251', 'c', 'm', 't'}: "Comments",
	{'\251', 'w', 'r', 't'}: "Composer",
	{'c', 'p', 'r', 't'}:    "Copyright",
	{'c', 'o', 'v', 'r'}:    "CoverArt",
	{'\251', 'g', 'e', 'n'}: "Genre", //check for the gnre atom, can't coexis:"Genre":
	{'g', 'n', 'r', 'e'}:    "Gnre",  //uint16
	{'\251', 'n', 'a', 'm'}: "Title",
	{'\251', 'd', 'a', 'y'}: "Year",
	{'t', 'r', 'k', 'n'}:    "TrackNumber", //2uint16 (track) (totaltracks:"TrackNumber":
	{'d', 'i', 's', 'k'}:    "DiscNumber",  //2uint16 (disc) (totaldiscs:"DiscNumber":
	{'\251', 't', 'o', 'o'}: "Encoder",
	{'t', 'm', 'p', 'o'}:    "BPM", //bigEndianUin:"BPM":
}

type MP4Tag struct {
	Album       string
	AlbumArtist string
	Artist      string
	BPM         int
	Comments    string
	Composer    string
	Copyright   string
	CoverArt    *image.Image
	Encoder     string
	Genre       string
	Title       string
	TrackNumber int
	TrackTotal  int
	DiscNumber  int
	DiscTotal   int
	Year        string
	
	reader      io.ReadSeeker
}

func (m *MP4Tag) ClearAllTags() {
	m.Album = ""
	m.AlbumArtist = ""
	m.Artist = ""
	m.BPM = 0
	m.Comments = ""
	m.Composer = ""
	m.Copyright = ""
	m.CoverArt = nil
	m.Encoder = ""
	m.Genre = ""
	m.Title = ""
	m.TrackNumber = 0
	m.TrackTotal = 0
	m.DiscNumber = 0
	m.DiscTotal = 0
	m.Year = ""
}

func (m *MP4Tag) GetAlbum() string {
	return m.Album
}

func (m *MP4Tag) GetAlbumArtist() string {
	return m.AlbumArtist
}

func (m *MP4Tag) GetArtist() string {
	return m.Artist
}

func (m *MP4Tag) GetBPM() int {
	return m.BPM
}

func (m *MP4Tag) GetComments() string {
	return m.Comments
}

func (m *MP4Tag) GetComposer() string {
	return m.Composer
}

func (m *MP4Tag) GetCopyright() string {
	return m.Copyright
}

func (m *MP4Tag) GetCoverArt() *image.Image {
	return m.CoverArt
}

func (m *MP4Tag) GetEncoder() string {
	return m.Encoder
}

func (m *MP4Tag) GetGenre() string {
	return m.Genre
}

func (m *MP4Tag) GetTitle() string {
	return m.Title
}

func (m *MP4Tag) GetTrackNumber() int {
	return m.TrackNumber
}

func (m *MP4Tag) GetTrackTotal() int {
	return m.TrackTotal
}

func (m *MP4Tag) GetDiscNumber() int {
	return m.DiscNumber
}

func (m *MP4Tag) GetDiscTotal() int {
	return m.DiscTotal
}

func (m *MP4Tag) GetYear() int {
	year, err := strconv.Atoi(m.Year)
	if err != nil {
		return 0
	}
	return year
}

func (m *MP4Tag) SetAlbum(album string) {
	m.Album = album
}
func (m *MP4Tag) SetAlbumArtist(albumArtist string) {
	m.AlbumArtist = albumArtist
}
func (m *MP4Tag) SetArtist(artist string) {
	m.Artist = artist
}
func (m *MP4Tag) SetBPM(bpm int) {
	m.BPM = bpm
}
func (m *MP4Tag) SetComments(comments string) {
	m.Comments = comments
}
func (m *MP4Tag) SetComposer(composer string) {
	m.Composer = composer
}
func (m *MP4Tag) SetCopyright(copyright string) {
	m.Copyright = copyright
}
func (m *MP4Tag) SetCoverArt(coverArt *image.Image) {
	m.CoverArt = coverArt
}
func (m *MP4Tag) SetEncoder(encoder string) {
	m.Encoder = encoder
}
func (m *MP4Tag) SetGenre(genre string) {
	m.Genre = genre
}
func (m *MP4Tag) SetTitle(title string) {
	m.Title = title
}
func (m *MP4Tag) SetTrackNumber(trackNumber int) {
	m.TrackNumber = trackNumber
}
func (m *MP4Tag) SetTrackTotal(trackTotal int) {
	m.TrackTotal = trackTotal
}
func (m *MP4Tag) SetDiscNumber(discNumber int) {
	m.DiscNumber = discNumber
}
func (m *MP4Tag) SetDiscTotal(discTotal int) {
	m.DiscTotal = discTotal
}
func (m *MP4Tag) SetYear(year int) {
	m.Year = fmt.Sprint(year)
}

func (m *MP4Tag) Save(w io.Writer) error {
	return SaveMP4(m.reader, w, m)
}

// https://github.com/FFmpeg/FFmpeg/blob/4e5523c98597a417eb43555933b1075d18ec5f8b/libavformat/id3v1.c#L278
var Id3v1GenreStr = map[int]string{
	0:   "Blues",
	1:   "Classic Rock",
	2:   "Country",
	3:   "Dance",
	4:   "Disco",
	5:   "Funk",
	6:   "Grunge",
	7:   "Hip-Hop",
	8:   "Jazz",
	9:   "Metal",
	10:  "New Age",
	11:  "Oldies",
	12:  "Other",
	13:  "Pop",
	14:  "R&B",
	15:  "Rap",
	16:  "Reggae",
	17:  "Rock",
	18:  "Techno",
	19:  "Industrial",
	20:  "Alternative",
	21:  "Ska",
	22:  "Death Metal",
	23:  "Pranks",
	24:  "Soundtrack",
	25:  "Euro-Techno",
	26:  "Ambient",
	27:  "Trip-Hop",
	28:  "Vocal",
	29:  "Jazz+Funk",
	30:  "Fusion",
	31:  "Trance",
	32:  "Classical",
	33:  "Instrumental",
	34:  "Acid",
	35:  "House",
	36:  "Game",
	37:  "Sound Clip",
	38:  "Gospel",
	39:  "Noise",
	40:  "AlternRock",
	41:  "Bass",
	42:  "Soul",
	43:  "Punk",
	44:  "Space",
	45:  "Meditative",
	46:  "Instrumental Pop",
	47:  "Instrumental Rock",
	48:  "Ethnic",
	49:  "Gothic",
	50:  "Darkwave",
	51:  "Techno-Industrial",
	52:  "Electronic",
	53:  "Pop-Folk",
	54:  "Eurodance",
	55:  "Dream",
	56:  "Southern Rock",
	57:  "Comedy",
	58:  "Cult",
	59:  "Gangsta",
	60:  "Top 40",
	61:  "Christian Rap",
	62:  "Pop/Funk",
	63:  "Jungle",
	64:  "Native American",
	65:  "Cabaret",
	66:  "New Wave",
	67:  "Psychedelic",
	68:  "Rave",
	69:  "Showtunes",
	70:  "Trailer",
	71:  "Lo-Fi",
	72:  "Tribal",
	73:  "Acid Punk",
	74:  "Acid Jazz",
	75:  "Polka",
	76:  "Retro",
	77:  "Musical",
	78:  "Rock & Roll",
	79:  "Hard Rock",
	80:  "Folk",
	81:  "Folk-Rock",
	82:  "National Folk",
	83:  "Swing",
	84:  "Fast Fusion",
	85:  "Bebop",
	86:  "Latin",
	87:  "Revival",
	88:  "Celtic",
	89:  "Bluegrass",
	90:  "Avantgarde",
	91:  "Gothic Rock",
	92:  "Progressive Rock",
	93:  "Psychedelic Rock",
	94:  "Symphonic Rock",
	95:  "Slow Rock",
	96:  "Big Band",
	97:  "Chorus",
	98:  "Easy Listening",
	99:  "Acoustic",
	100: "Humour",
	101: "Speech",
	102: "Chanson",
	103: "Opera",
	104: "Chamber Music",
	105: "Sonata",
	106: "Symphony",
	107: "Booty Bass",
	108: "Primus",
	109: "Porn Groove",
	110: "Satire",
	111: "Slow Jam",
	112: "Club",
	113: "Tango",
	114: "Samba",
	115: "Folklore",
	116: "Ballad",
	117: "Power Ballad",
	118: "Rhythmic Soul",
	119: "Freestyle",
	120: "Duet",
	121: "Punk Rock",
	122: "Drum Solo",
	123: "A Cappella",
	124: "Euro-House",
	125: "Dance Hall",
	126: "Goa",
	127: "Drum & Bass",
	128: "Club-House",
	129: "Hardcore Techno",
	130: "Terror",
	131: "Indie",
	132: "BritPop",
	133: "Negerpunk",
	134: "Polsk Punk",
	135: "Beat",
	136: "Christian Gangsta Rap",
	137: "Heavy Metal",
	138: "Black Metal",
	139: "Crossover",
	140: "Contemporary Christian",
	141: "Christian Rock",
	142: "Merengue",
	143: "Salsa",
	144: "Thrash Metal",
	145: "Anime",
	146: "Jpop",
	147: "Synthpop",
	148: "Abstract",
	149: "Art Rock",
	150: "Baroque",
	151: "Bhangra",
	152: "Big Beat",
	153: "Breakbeat",
	154: "Chillout",
	155: "Downtempo",
	156: "Dub",
	157: "EBM",
	158: "Eclectic",
	159: "Electro",
	160: "Electroclash",
	161: "Emo",
	162: "Experimental",
	163: "Garage",
	164: "Global",
	165: "IDM",
	166: "Illbient",
	167: "Industro-Goth",
	168: "Jam Band",
	169: "Krautrock",
	170: "Leftfield",
	171: "Lounge",
	172: "Math Rock",
	173: "New Romantic",
	174: "Nu-Breakz",
	175: "Post-Punk",
	176: "Post-Rock",
	177: "Psytrance",
	178: "Shoegaze",
	179: "Space Rock",
	180: "Trop Rock",
	181: "World Music",
	182: "Neoclassical",
	183: "Audiobook",
	184: "Audio Theatre",
	185: "Neue Deutsche Welle",
	186: "Podcast",
	187: "Indie Rock",
	188: "G-Funk",
	189: "Dubstep",
	190: "Garage Rock",
	191: "Psybient",
}
