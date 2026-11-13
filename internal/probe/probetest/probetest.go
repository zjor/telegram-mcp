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

// MP4Track describes one track of a synthetic MP4 file.
type MP4Track struct {
	Handler  string // "vide" or "soun"
	Codec    string // sample entry fourcc, such as "avc1" or "mp4a"
	W, H     int    // stored size in pixels
	Rotate90 bool   // write a 90-degree rotation matrix
}

// MP4File returns a minimal MP4 file: ftyp, moov and a small mdat. If moovLast
// is true, mdat comes before moov. The mvhd box uses the given timescale and
// duration.
func MP4File(moovLast bool, timescale, duration uint32, tracks ...MP4Track) []byte {
	mvhd := make([]byte, 100)
	binary.BigEndian.PutUint32(mvhd[12:], timescale)
	binary.BigEndian.PutUint32(mvhd[16:], duration)
	moovBody := mp4Box("mvhd", mvhd)
	for _, t := range tracks {
		moovBody = append(moovBody, mp4Trak(t)...)
	}
	ftyp := mp4Box("ftyp", append([]byte("isom\x00\x00\x02\x00"), "isom"...))
	moov := mp4Box("moov", moovBody)
	mdat := mp4Box("mdat", []byte{1, 2, 3, 4})
	out := ftyp
	if moovLast {
		out = append(out, mdat...)
		return append(out, moov...)
	}
	out = append(out, moov...)
	return append(out, mdat...)
}

func mp4Trak(t MP4Track) []byte {
	tkhd := make([]byte, 84)
	// Matrix a b u c d v x y w; a is at 40, b at 44, c at 52, d at 56.
	if t.Rotate90 {
		binary.BigEndian.PutUint32(tkhd[44:], 0x10000)    // b = 1.0
		binary.BigEndian.PutUint32(tkhd[52:], 0xffff0000) // c = -1.0
	} else {
		binary.BigEndian.PutUint32(tkhd[40:], 0x10000) // a = 1.0
		binary.BigEndian.PutUint32(tkhd[56:], 0x10000) // d = 1.0
	}
	binary.BigEndian.PutUint32(tkhd[76:], uint32(t.W)<<16)
	binary.BigEndian.PutUint32(tkhd[80:], uint32(t.H)<<16)

	hdlr := make([]byte, 24)
	copy(hdlr[8:12], t.Handler)

	entry := mp4Box(t.Codec, make([]byte, 8))
	stsd := append([]byte{0, 0, 0, 0, 0, 0, 0, 1}, entry...)

	stbl := mp4Box("stbl", mp4Box("stsd", stsd))
	minf := mp4Box("minf", stbl)
	mdia := mp4Box("mdia", append(mp4Box("hdlr", hdlr), minf...))
	return mp4Box("trak", append(mp4Box("tkhd", tkhd), mdia...))
}

func mp4Box(typ string, body []byte) []byte {
	out := binary.BigEndian.AppendUint32(nil, uint32(8+len(body)))
	out = append(out, typ...)
	return append(out, body...)
}
