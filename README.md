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

Read from a seekable source and keep it open through Save. The legacy writer
keeps untouched iTunes items and their encoded artwork. Setting a new cover
through this API encodes it as PNG. Legacy Save buffers its output and supports
a single movie and media box. It rejects fragmented files and 64-bit chunk
offset relocation. File writes are non-atomic.

## Canonical metadata

Use `OpenMetadata` with the [AudioMeta field contract](https://github.com/gcottom/audiometa-core)
to access typed fields, multiple values, and custom properties independently of
the MP4 atom layout.

```go
// Import github.com/gcottom/audiometa-core as core.
metadata, err := mp4meta.OpenMetadata(input)
if err != nil {
    return err
}
if err := metadata.SetField(core.TrackNumber, core.IntValue(7)); err != nil {
    return err
}
if err := metadata.SetCustom("COLLECTION", "Favorites", "Listening queue"); err != nil {
    return err
}
return metadata.Save(output)
```

- The editor writes movie-scope iTunes metadata. Untouched items retain their
  raw bytes and relative order, including private types, localized values, and
  repeat entries. Recognized fields use the registry's primary carriers.
  Clearing a field removes its mapped aliases. Setting a field through its
  primary carrier keeps conflicting aliases unless the old carrier would
  shadow a required fallback.
- Custom metadata and lossless extensions use `----` freeform items under
  `io.github.gcottom.audiometa`. Other applications may display their raw values.
  JPEG and PNG artwork use `covr` where possible. The editor also stores richer
  picture attributes in the private value and uses them when the conventional
  cover bytes still match. A new conventional cover supplied by another editor
  takes precedence on reopen if the encoded artwork differs.
- Nero chapter lists represent ordered start times and titles when those fit
  the chapter box. The private extension preserves chapter IDs and end times.
  QuickTime chapter tracks and multiple existing chapter lists are preserved
  during unrelated edits. Chapter changes on those files return an error.
- `ClearAll(true)` preserves unknown items. `ClearAll(false)` may remove unknown
  items in the movie-scope iTunes item list. Other metadata collections,
  playback trim, codec properties, and technical structures are preserved.
- The editor streams media bytes and updates local 32-bit and 64-bit chunk
  offsets when they move. It rejects fragmented edits, unsafe index
  relocation, and adding a competing movie-scope metadata handler. The
  iTunes item list and relocated offset tables are each limited to 64 MiB.
  Unsupported or undecodable items remain opaque outside targeted edits.
- Keep the seekable source open and unchanged while editing and saving.
  Use a distinct destination. The streaming `Save` writes at the writer's
  current position, leaves both resources open, and can leave partial output
  after an I/O failure. Use a separate file and replace the original only
  after success. Editors and their source readers are single-caller objects.

The shared contract runs against an AAC/M4A fast-start fixture. With FFmpeg
and FFprobe on PATH and Mutagen installed for Python, the integration test
also checks sample-identical decoded PCM, unchanged duration and audio
configuration, readable chapters, native and freeform values, repeated text,
and exact JPEG cover data.
