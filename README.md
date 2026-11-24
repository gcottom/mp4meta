# MP4META
[![Coverage Status](https://coveralls.io/repos/github/gcottom/mp4meta/badge.svg?branch=main)](https://coveralls.io/github/gcottom/mp4meta?branch=main)

MP4Meta is a high level implementation of abema's go-mp4, licensed under the MIT license, link in Acknowledgements. 
This library is both an adapter and facade implementation for abema's low level I/O interfaces. By utilizing this library,
we are able to interact with almost all relevent m4a /m4b metadata tags. At the same time, we are able to keep mdat in sync
when editing the metadata.

## Features
- Read and write MP4 atoms (m4a, m4b): "artist", "albumArtist", "album", "coverArt", "comments", "composer", "copyright", "genre", 
"title", "year", "encoder"
- Reads and writes "trackNumber", "trackTotal", "discNumber", "discTotal" and "tempo (bpm)" tags with ease
- Everything's built in, plug and play, with a simple interface, compatible with [audiometa v3](https://github.com/gcottom/audiometa/v3),
for more audio formats. 

## Acknowledgements
- [go-mp4](https://github.com/abema/go-mp4): Abema's go-mp4 makes this library possible. They provided the low level and I made it high level,
for m4a, and m4b. With this combination, we can write meta tags with ease. 

## License
This project is licensed under the MIT License. See [LICENSE.md](LICENSE.md) for details.

Parts of this project include third-party libraries under the MIT license. See [LICENSE.md](LICENSE.md) for details.

## Related Links
[audiometa v3](https://github.com/gcottom/audiometa/v3)

[mp3meta](https://github.com/gcottom/mp3meta)

[oggmeta](https://github.com/gcottom/oggmeta)

[flacmeta](https://github.com/gcottom/flacmeta)
## Example

```go
in, err := os.Open("input.m4a")
if err != nil { return err }
defer in.Close()

tag, err := mp4meta.ReadMP4(in)
if err != nil { return err }
tag.SetTitle("New title")

var output bytes.Buffer
if err := tag.Save(&output); err != nil { return err }
return os.WriteFile("output.m4a", output.Bytes(), 0644)
```

Read from a seekable source and keep it open through Save. Save buffers the
entire rewritten file and is not atomic; prefer a separate output file and
validate it before replacing the input. Save is intended for non-fragmented
iTunes-style audio files with one movie box and one media data box; relocating
64-bit chunk offsets is not supported. The writer replaces the item list with
supported fields, so unsupported/private item-list data may be lost. Covers
are encoded as PNG. Fast-start AAC/M4A is covered by a fixture that verifies
the relocated 32-bit chunk offset and byte-identical media payload.
