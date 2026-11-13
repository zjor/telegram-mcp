package probe

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// MP4Info describes an MP4 (ISO base media) file.
type MP4Info struct {
	VideoCodec string  // sample entry fourcc of the first video track, "" if none
	AudioCodec string  // sample entry fourcc of the first audio track, "" if none
	Width      int     // displayed width of the video track, after rotation
	Height     int     // displayed height of the video track, after rotation
	Duration   float64 // seconds, from the movie header or mvex/mehd; 0 if unknown
}

var errMP4Truncated = errors.New("mp4: truncated box")

// MP4 reads track codecs, displayed size and duration from an MP4 file of the
// given size. A file without an ftyp or moov box gives ErrUnknownFormat.
func MP4(r io.ReaderAt, size int64) (MP4Info, error) {
	var info MP4Info
	var sawFtyp, sawMoov bool
	haveVideo, haveAudio := false, false
	var timescale uint32
	var fragDur uint64

	err := walkBoxes(r, 0, size, size, func(typ string, bs, be int64, truncated bool) error {
		switch typ {
		case "ftyp":
			sawFtyp = true
		case "moov":
			if truncated {
				return errMP4Truncated
			}
			sawMoov = true
			return walkBoxes(r, bs, be, size, func(typ string, bs, be int64, truncated bool) error {
				if truncated {
					return errMP4Truncated
				}
				switch typ {
				case "mvhd":
					ts, dur, err := parseMvhd(r, bs, be, size)
					if err != nil {
						return err
					}
					timescale = ts
					if ts != 0 {
						info.Duration = float64(dur) / float64(ts)
					}
				case "mvex":
					return walkPath(r, bs, be, size, []string{"mehd"}, func(bs, be int64) error {
						d, err := parseMehd(r, bs, be, size)
						fragDur = d
						return err
					})
				case "trak":
					t, err := parseTrak(r, bs, be, size)
					if err != nil {
						return err
					}
					switch {
					case t.handler == "vide" && !haveVideo:
						haveVideo = true
						info.VideoCodec = t.codec
						info.Width, info.Height = t.w, t.h
					case t.handler == "soun" && !haveAudio:
						haveAudio = true
						info.AudioCodec = t.codec
					}
				}
				return nil
			})
		}
		return nil
	})
	if err != nil {
		if !sawFtyp && !sawMoov {
			return MP4Info{}, ErrUnknownFormat
		}
		return MP4Info{}, err
	}
	if !sawFtyp || !sawMoov {
		return MP4Info{}, ErrUnknownFormat
	}
	// Fragmented files (screen recorders, ffmpeg -movflags frag_keyframe)
	// write 0 in mvhd and keep the total in mvex/mehd.
	if info.Duration == 0 && timescale != 0 && fragDur != 0 {
		info.Duration = float64(fragDur) / float64(timescale)
	}
	return info, nil
}

type mp4Track struct {
	handler, codec string
	w, h           int
}

func parseMvhd(r io.ReaderAt, bs, be, size int64) (timescale uint32, dur uint64, err error) {
	var b [32]byte
	n := int(min64(int64(len(b)), be-bs))
	if n < 20 {
		return 0, 0, fmt.Errorf("mp4: short mvhd")
	}
	if err := readFull(r, b[:n], bs, size); err != nil {
		return 0, 0, fmt.Errorf("mp4: mvhd: %w", err)
	}
	if b[0] == 1 {
		if n < 32 {
			return 0, 0, fmt.Errorf("mp4: short mvhd")
		}
		return binary.BigEndian.Uint32(b[20:24]), binary.BigEndian.Uint64(b[24:32]), nil
	}
	return binary.BigEndian.Uint32(b[12:16]), uint64(binary.BigEndian.Uint32(b[16:20])), nil
}

// parseMehd returns fragment_duration in movie timescale units.
func parseMehd(r io.ReaderAt, bs, be, size int64) (uint64, error) {
	var b [12]byte
	if be-bs < 1 {
		return 0, fmt.Errorf("mp4: short mehd")
	}
	if err := readFull(r, b[:1], bs, size); err != nil {
		return 0, fmt.Errorf("mp4: mehd: %w", err)
	}
	if b[0] == 1 {
		if be-bs < 12 {
			return 0, fmt.Errorf("mp4: short mehd")
		}
		if err := readFull(r, b[:12], bs, size); err != nil {
			return 0, fmt.Errorf("mp4: mehd: %w", err)
		}
		return binary.BigEndian.Uint64(b[4:12]), nil
	}
	if be-bs < 8 {
		return 0, fmt.Errorf("mp4: short mehd")
	}
	if err := readFull(r, b[:8], bs, size); err != nil {
		return 0, fmt.Errorf("mp4: mehd: %w", err)
	}
	return uint64(binary.BigEndian.Uint32(b[4:8])), nil
}

func parseTrak(r io.ReaderAt, bs, be, size int64) (mp4Track, error) {
	var t mp4Track
	err := walkBoxes(r, bs, be, size, func(typ string, bs, be int64, truncated bool) error {
		if truncated {
			return errMP4Truncated
		}
		switch typ {
		case "tkhd":
			w, h, err := parseTkhd(r, bs, be, size)
			if err != nil {
				return err
			}
			t.w, t.h = w, h
		case "mdia":
			return walkBoxes(r, bs, be, size, func(typ string, bs, be int64, truncated bool) error {
				if truncated {
					return errMP4Truncated
				}
				switch typ {
				case "hdlr":
					var b [12]byte
					if be-bs < 12 {
						return fmt.Errorf("mp4: short hdlr")
					}
					if err := readFull(r, b[:], bs, size); err != nil {
						return fmt.Errorf("mp4: hdlr: %w", err)
					}
					t.handler = string(b[8:12])
				case "minf":
					return walkPath(r, bs, be, size, []string{"stbl", "stsd"}, func(bs, be int64) error {
						var b [16]byte
						if be-bs < 16 {
							return fmt.Errorf("mp4: short stsd")
						}
						if err := readFull(r, b[:], bs, size); err != nil {
							return fmt.Errorf("mp4: stsd: %w", err)
						}
						t.codec = string(b[12:16])
						return nil
					})
				}
				return nil
			})
		}
		return nil
	})
	return t, err
}

// parseTkhd returns the displayed width and height, swapped for a 90 or 270
// degree rotation matrix.
func parseTkhd(r io.ReaderAt, bs, be, size int64) (int, int, error) {
	var b [96]byte
	if be-bs < 1 {
		return 0, 0, fmt.Errorf("mp4: short tkhd")
	}
	if err := readFull(r, b[:1], bs, size); err != nil {
		return 0, 0, fmt.Errorf("mp4: tkhd: %w", err)
	}
	// Fixed part before the matrix: version 0 has 40 bytes, version 1 has 52.
	off := 40
	if b[0] == 1 {
		off = 52
	}
	need := off + 36 + 8
	if be-bs < int64(need) {
		return 0, 0, fmt.Errorf("mp4: short tkhd")
	}
	if err := readFull(r, b[:need], bs, size); err != nil {
		return 0, 0, fmt.Errorf("mp4: tkhd: %w", err)
	}
	a := int32(binary.BigEndian.Uint32(b[off:]))
	bb := int32(binary.BigEndian.Uint32(b[off+4:]))
	c := int32(binary.BigEndian.Uint32(b[off+12:]))
	d := int32(binary.BigEndian.Uint32(b[off+16:]))
	w := int(binary.BigEndian.Uint32(b[off+36:]) >> 16)
	h := int(binary.BigEndian.Uint32(b[off+40:]) >> 16)
	if a == 0 && d == 0 && bb != 0 && c != 0 {
		w, h = h, w
	}
	return w, h, nil
}

// walkPath descends through the named child boxes and calls fn with the body
// range of the last one, if it exists.
func walkPath(r io.ReaderAt, start, end, size int64, path []string, fn func(bs, be int64) error) error {
	return walkBoxes(r, start, end, size, func(typ string, bs, be int64, truncated bool) error {
		if typ != path[0] {
			return nil
		}
		if truncated {
			return errMP4Truncated
		}
		if len(path) == 1 {
			return fn(bs, be)
		}
		return walkPath(r, bs, be, size, path[1:], fn)
	})
}

// walkBoxes calls fn for each box in [start, end). The offsets passed to fn
// are the body range. truncated is true if the box claims to end after the
// parent. A truncated box ends the walk. A box smaller than its header is an
// error, so the walk always moves forward.
func walkBoxes(r io.ReaderAt, start, end, size int64, fn func(typ string, bs, be int64, truncated bool) error) error {
	if end > size {
		end = size
	}
	for pos := start; pos < end; {
		var h [16]byte
		hlen := int64(8)
		if end-pos < hlen {
			return errMP4Truncated
		}
		if err := readFull(r, h[:8], pos, size); err != nil {
			return fmt.Errorf("mp4: box header: %w", err)
		}
		boxSize := int64(binary.BigEndian.Uint32(h[:4]))
		typ := string(h[4:8])
		switch boxSize {
		case 1:
			hlen = 16
			if end-pos < hlen {
				return errMP4Truncated
			}
			if err := readFull(r, h[8:16], pos+8, size); err != nil {
				return fmt.Errorf("mp4: box header: %w", err)
			}
			u := binary.BigEndian.Uint64(h[8:16])
			if u > uint64(size) {
				boxSize = size + 1 // larger than any parent: reported as truncated
			} else {
				boxSize = int64(u)
			}
		case 0:
			boxSize = end - pos
		}
		if boxSize < hlen {
			return fmt.Errorf("mp4: box %q size %d smaller than header", typ, boxSize)
		}
		boxEnd := pos + boxSize
		truncated := boxEnd > end
		if truncated {
			boxEnd = end
		}
		if err := fn(typ, pos+hlen, boxEnd, truncated); err != nil {
			return err
		}
		if truncated {
			return nil
		}
		pos = boxEnd
	}
	return nil
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
