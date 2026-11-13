package probe

import (
	"encoding/binary"
	"io"
)

const mp3SearchWindow = 64 << 10

var (
	mp3V1Bitrates = [16]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}
	mp3V2Bitrates = [16]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}
	mp3V1Rates    = [4]int{44100, 48000, 32000, 0}
	mp3V2Rates    = [4]int{22050, 24000, 16000, 0}
	mp3V25Rates   = [4]int{11025, 12000, 8000, 0}
)

// mp3Header holds the fields of an MPEG Layer III frame header.
type mp3Header struct {
	bitrate, rate, length int
	samples               int // samples per frame
	xingOff               int // offset of the Xing/Info tag from the frame start
}

// parseMP3Header decodes b (at least 4 bytes). It accepts MPEG-1, 2 and 2.5
// Layer III with valid bitrate and sample rate indexes.
func parseMP3Header(b []byte) (mp3Header, bool) {
	if len(b) < 4 || b[0] != 0xFF || b[1]&0xE0 != 0xE0 || b[1]>>1&3 != 1 { // sync, Layer III
		return mp3Header{}, false
	}
	var h mp3Header
	mono := b[3]>>6 == 3
	var br, rate int
	switch b[1] >> 3 & 3 {
	case 3: // MPEG-1
		br, rate = mp3V1Bitrates[b[2]>>4], mp3V1Rates[b[2]>>2&3]
		h.samples, h.xingOff = 1152, 36
		if mono {
			h.xingOff = 21
		}
	case 2, 0: // MPEG-2, MPEG-2.5
		br = mp3V2Bitrates[b[2]>>4]
		rate = mp3V2Rates[b[2]>>2&3]
		if b[1]>>3&3 == 0 {
			rate = mp3V25Rates[b[2]>>2&3]
		}
		h.samples, h.xingOff = 576, 21
		if mono {
			h.xingOff = 13
		}
	default:
		return mp3Header{}, false
	}
	if br == 0 || rate == 0 {
		return mp3Header{}, false
	}
	h.bitrate, h.rate = br, rate
	h.length = h.samples/8*br*1000/rate + int(b[2]>>1&1)
	return h, true
}

// MP3 returns the duration in seconds of an MPEG-1/2/2.5 Layer III file, or
// ErrUnknownFormat. It skips an ID3v2 tag, searches up to 64 KiB for a frame
// and accepts it only if a second frame follows or a Xing/Info header is
// present. A Xing frame count gives the duration; otherwise it is estimated
// from the bitrate of the first frame.
func MP3(r io.ReaderAt, size int64) (float64, error) {
	if size < 4 {
		return 0, ErrUnknownFormat
	}
	var start int64
	var tag [10]byte
	if size >= 10 && readFull(r, tag[:], 0, size) == nil && string(tag[:3]) == "ID3" &&
		tag[6]&0x80 == 0 && tag[7]&0x80 == 0 && tag[8]&0x80 == 0 && tag[9]&0x80 == 0 {
		start = 10 + (int64(tag[6])<<21 | int64(tag[7])<<14 | int64(tag[8])<<7 | int64(tag[9]))
		if tag[5]&0x10 != 0 {
			start += 10 // footer
		}
	}
	if start >= size {
		return 0, ErrUnknownFormat
	}
	n := size - start
	if n > mp3SearchWindow+4096 {
		n = mp3SearchWindow + 4096
	}
	buf := make([]byte, n)
	if err := readFull(r, buf, start, size); err != nil {
		return 0, err
	}
	for i := 0; i < len(buf)-3 && i <= mp3SearchWindow; i++ {
		h, ok := parseMP3Header(buf[i:])
		if !ok {
			continue
		}
		if frames, ok := xingFrames(buf[i:], h); ok {
			return float64(frames) * float64(h.samples) / float64(h.rate), nil
		}
		next, ok := parseMP3Header(bufAt(buf, i+h.length))
		if !ok || next.rate != h.rate {
			continue
		}
		audio := size - (start + int64(i))
		return float64(audio) * 8 / float64(h.bitrate*1000), nil
	}
	return 0, ErrUnknownFormat
}

func bufAt(b []byte, i int) []byte {
	if i < 0 || i > len(b) {
		return nil
	}
	return b[i:]
}

// xingFrames reads the frame count from a Xing/Info header in frame f.
func xingFrames(f []byte, h mp3Header) (uint32, bool) {
	off := h.xingOff
	if len(f) < off+12 {
		return 0, false
	}
	tag := string(f[off : off+4])
	if (tag == "Xing" || tag == "Info") && f[off+7]&1 != 0 {
		if frames := binary.BigEndian.Uint32(f[off+8:]); frames > 0 {
			return frames, true
		}
	}
	return 0, false
}
