// Package probetest builds synthetic media files for tests of package probe
// and of code that uses it.
package probetest

import "encoding/binary"

// OggFile returns a minimal OGG file with two pages. The first page holds the
// codec header packet. The last page has the given granule position. CRC
// fields are zero. codec is "opus" or "vorbis"; any other value is written as
// an 8-byte codec name. For "opus", rate is the input rate and preSkip the
// pre-skip. For "vorbis", rate is the sample rate and preSkip is ignored.
func OggFile(codec string, rate int, preSkip int, lastGranule int64) []byte {
	var head []byte
	switch codec {
	case "opus":
		head = append(head, "OpusHead"...)
		head = append(head, 1, 2) // version, channels
		head = binary.LittleEndian.AppendUint16(head, uint16(preSkip))
		head = binary.LittleEndian.AppendUint32(head, uint32(rate))
		head = append(head, 0, 0, 0) // gain, mapping family
	case "vorbis":
		head = append(head, 1)
		head = append(head, "vorbis"...)
		head = binary.LittleEndian.AppendUint32(head, 0) // version
		head = append(head, 2)                           // channels
		head = binary.LittleEndian.AppendUint32(head, uint32(rate))
		head = append(head, make([]byte, 12)...) // bitrates
		head = append(head, 0xb8, 1)             // blocksizes, framing
	default:
		head = append(head, (codec + "        ")[:8]...)
	}
	out := oggPage(nil, 0x02, 0, 0, head)
	return oggPage(out, 0x04, lastGranule, 1, []byte{0})
}

func oggPage(dst []byte, flags byte, granule int64, seq uint32, packet []byte) []byte {
	dst = append(dst, "OggS"...)
	dst = append(dst, 0, flags)
	dst = binary.LittleEndian.AppendUint64(dst, uint64(granule))
	dst = binary.LittleEndian.AppendUint32(dst, 1) // serial
	dst = binary.LittleEndian.AppendUint32(dst, seq)
	dst = binary.LittleEndian.AppendUint32(dst, 0) // crc
	dst = append(dst, 1, byte(len(packet)))        // one segment
	return append(dst, packet...)
}
