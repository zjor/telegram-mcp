package probe

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

// OggInfo describes an OGG audio file.
type OggInfo struct {
	Codec    string  // "opus", "vorbis", or the first 8 bytes of another codec's header
	Duration float64 // seconds; 0 for other codecs
}

const (
	oggPageHeaderLen = 27
	oggTailWindow    = 64 << 10
)

// Ogg reads the codec and duration of an OGG file of the given size. It does
// not check CRCs. A non-OGG file gives ErrUnknownFormat. An OGG file with a
// codec other than Opus or Vorbis gives only the Codec field.
func Ogg(r io.ReaderAt, size int64) (OggInfo, error) {
	var magic [4]byte
	if n, err := r.ReadAt(magic[:], 0); n < 4 {
		if err == io.EOF || err == nil {
			return OggInfo{}, ErrUnknownFormat
		}
		return OggInfo{}, err
	}
	if string(magic[:]) != "OggS" {
		return OggInfo{}, ErrUnknownFormat
	}

	// First page: header, segment table, first packet.
	var hdr [oggPageHeaderLen]byte
	if err := readFull(r, hdr[:], 0, size); err != nil {
		return OggInfo{}, fmt.Errorf("ogg: first page header: %w", err)
	}
	nseg := int(hdr[26])
	table := make([]byte, nseg)
	if err := readFull(r, table, oggPageHeaderLen, size); err != nil {
		return OggInfo{}, fmt.Errorf("ogg: segment table: %w", err)
	}
	pktLen := 0
	for _, s := range table {
		pktLen += int(s)
		if s < 255 {
			break
		}
	}
	if pktLen > 64 {
		pktLen = 64
	}
	pkt := make([]byte, pktLen)
	if err := readFull(r, pkt, int64(oggPageHeaderLen+nseg), size); err != nil {
		return OggInfo{}, fmt.Errorf("ogg: first packet: %w", err)
	}

	var info OggInfo
	var preSkip uint16
	var rate uint32
	switch {
	case bytes.HasPrefix(pkt, []byte("OpusHead")):
		if len(pkt) < 12 {
			return OggInfo{}, fmt.Errorf("ogg: short OpusHead")
		}
		info.Codec = "opus"
		preSkip = binary.LittleEndian.Uint16(pkt[10:12])
	case bytes.HasPrefix(pkt, []byte("\x01vorbis")):
		if len(pkt) < 16 {
			return OggInfo{}, fmt.Errorf("ogg: short vorbis header")
		}
		info.Codec = "vorbis"
		rate = binary.LittleEndian.Uint32(pkt[12:16])
	default:
		n := len(pkt)
		if n > 8 {
			n = 8
		}
		info.Codec = string(pkt[:n])
		return info, nil
	}

	granule, err := lastGranule(r, size)
	if err != nil {
		return OggInfo{}, err
	}
	if granule < 0 {
		return OggInfo{}, fmt.Errorf("ogg: no final granule position")
	}
	if info.Codec == "opus" {
		if granule > int64(preSkip) {
			info.Duration = float64(granule-int64(preSkip)) / 48000
		}
		return info, nil
	}
	if rate == 0 {
		return OggInfo{}, fmt.Errorf("ogg: vorbis sample rate is zero")
	}
	info.Duration = float64(granule) / float64(rate)
	return info, nil
}

// lastGranule finds the last page in the final 64 KiB and returns its granule.
func lastGranule(r io.ReaderAt, size int64) (int64, error) {
	start := size - oggTailWindow
	if start < 0 {
		start = 0
	}
	buf := make([]byte, size-start)
	if err := readFull(r, buf, start, size); err != nil {
		return 0, fmt.Errorf("ogg: tail: %w", err)
	}
	for i := len(buf) - oggPageHeaderLen; i >= 0; i-- {
		if string(buf[i:i+4]) == "OggS" && buf[i+4] == 0 {
			return int64(binary.LittleEndian.Uint64(buf[i+6 : i+14])), nil
		}
	}
	return 0, fmt.Errorf("ogg: last page not found")
}

// readFull fills b from offset off, failing if it would pass size.
func readFull(r io.ReaderAt, b []byte, off, size int64) error {
	if off+int64(len(b)) > size {
		return io.ErrUnexpectedEOF
	}
	n, err := r.ReadAt(b, off)
	if n == len(b) {
		return nil
	}
	if err == nil || err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return err
}
