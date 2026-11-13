package probe

import (
	"encoding/binary"
	"io"
)

// FLAC returns the duration in seconds of a FLAC file from its STREAMINFO
// block. Duration is 0 if the total sample count is unknown. A non-FLAC file
// gives ErrUnknownFormat.
func FLAC(r io.ReaderAt, size int64) (float64, error) {
	var b [26]byte
	if size < int64(len(b)) {
		return 0, ErrUnknownFormat
	}
	if err := readFull(r, b[:], 0, size); err != nil {
		return 0, err
	}
	// "fLaC", block header (type 0 = STREAMINFO, length 34), 34-byte body.
	if string(b[:4]) != "fLaC" || b[4]&0x7f != 0 || b[5] != 0 || b[6] != 0 || b[7] < 34 {
		return 0, ErrUnknownFormat
	}
	v := binary.BigEndian.Uint64(b[18:])
	rate := v >> 44
	samples := v & (1<<36 - 1)
	if rate == 0 || samples == 0 {
		return 0, nil
	}
	return float64(samples) / float64(rate), nil
}
