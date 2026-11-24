package mp4meta

func getInt(b []byte) int {
	var n int
	for _, x := range b {
		n = n << 8
		n |= int(x)
	}
	return n
}

// Older MP4Meta releases wrote a 16-bit tempo followed by two padding
// bytes inside a signed integer atom. Decode the historical layout as
// 16-bit when it would otherwise look like an unusually large tempo.
func legacyTempoPadding(data []byte) bool {
	return len(data) == 4 && (data[0] != 0 || data[1] != 0) && data[2] == 0 && data[3] == 0
}
