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
	return mp4File(moovLast, timescale, duration, nil, tracks)
}

// MP4FileFragmented returns an MP4 file like one from a screen recorder: the
// mvhd duration is 0. If withMehd is true, moov holds mvex/mehd with the given
// fragment duration in timescale units. Otherwise moov has no mehd.
func MP4FileFragmented(timescale, fragDuration uint32, withMehd bool, tracks ...MP4Track) []byte {
	var mvex []byte
	if withMehd {
		mehd := binary.BigEndian.AppendUint32(make([]byte, 4), fragDuration) // version 0, flags 0
		mvex = mp4Box("mvex", mp4Box("mehd", mehd))
	} else {
		mvex = mp4Box("mvex", mp4Box("trex", make([]byte, 24)))
	}
	return mp4File(false, timescale, 0, mvex, tracks)
}

func mp4File(moovLast bool, timescale, duration uint32, extra []byte, tracks []MP4Track) []byte {
	mvhd := make([]byte, 100)
	binary.BigEndian.PutUint32(mvhd[12:], timescale)
	binary.BigEndian.PutUint32(mvhd[16:], duration)
	moovBody := mp4Box("mvhd", mvhd)
	for _, t := range tracks {
		moovBody = append(moovBody, mp4Trak(t)...)
	}
	moovBody = append(moovBody, extra...)
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

// MP3File returns a synthetic MPEG-1 Layer III file at 44100 Hz, stereo. If
// id3 is true, it starts with an ID3v2 tag. If xingFrames > 0, the first
// frame holds a Xing header with that frame count. frames is the number of
// frames written, including the Xing frame.
func MP3File(id3 bool, xingFrames, bitrateKbps, frames int) []byte {
	return MP3FileOpts(MP3Opts{ID3: id3, Xing: xingFrames, BitrateKbps: bitrateKbps, Frames: frames})
}

// MP3Opts configures MP3FileOpts.
type MP3Opts struct {
	ID3         bool // start with an ID3v2 tag
	ID3Size     int  // tag body size in bytes; 0 means 32
	Version     int  // 1 (default), 2 or 25 for MPEG-2.5
	RateIdx     int  // sample rate index 0..2 within the version
	Mono        bool
	Xing        int // Xing frame count in the first frame; 0 for none
	BitrateKbps int
	Frames      int // frames written, including the Xing frame
}

var (
	mp3V1Bitrates = []int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}
	mp3V2Bitrates = []int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160}
	mp3Rates      = map[int][3]int{1: {44100, 48000, 32000}, 2: {22050, 24000, 16000}, 25: {11025, 12000, 8000}}
)

// MP3FileOpts returns a synthetic MPEG Layer III file.
func MP3FileOpts(o MP3Opts) []byte {
	if o.Version == 0 {
		o.Version = 1
	}
	var out []byte
	if o.ID3 {
		n := o.ID3Size
		if n == 0 {
			n = 32
		}
		out = append(out, "ID3\x03\x00\x00"...)
		out = append(out, byte(n>>21&0x7f), byte(n>>14&0x7f), byte(n>>7&0x7f), byte(n&0x7f))
		out = append(out, make([]byte, n)...)
	}
	table := mp3V1Bitrates
	verBits := byte(3)
	spf := 1152
	switch o.Version {
	case 2:
		table, verBits, spf = mp3V2Bitrates, 2, 576
	case 25:
		table, verBits, spf = mp3V2Bitrates, 0, 576
	}
	idx := byte(0)
	for i, kbps := range table {
		if kbps == o.BitrateKbps {
			idx = byte(i)
		}
	}
	rate := mp3Rates[o.Version][o.RateIdx]
	frameLen := spf / 8 * o.BitrateKbps * 1000 / rate
	// Xing offset after the 4-byte header: V1 stereo 32, V1 mono 17, V2 stereo 17, V2 mono 9.
	xingOff := 36
	if o.Version != 1 {
		xingOff = 21
	}
	b3 := byte(0)
	if o.Mono {
		b3 = 0xC0
		if o.Version == 1 {
			xingOff = 21
		} else {
			xingOff = 13
		}
	}
	for i := 0; i < o.Frames; i++ {
		f := make([]byte, frameLen)
		f[0], f[1], f[2], f[3] = 0xFF, 0xE0|verBits<<3|1<<1|1, idx<<4|byte(o.RateIdx)<<2, b3
		if i == 0 && o.Xing > 0 {
			copy(f[xingOff:], "Xing")
			binary.BigEndian.PutUint32(f[xingOff+4:], 1) // flags: frames present
			binary.BigEndian.PutUint32(f[xingOff+8:], uint32(o.Xing))
		}
		out = append(out, f...)
	}
	return out
}

// FLACFile returns a synthetic FLAC file with only a STREAMINFO block.
func FLACFile(rate int, samples int64) []byte {
	out := []byte("fLaC")
	out = append(out, 0x80, 0, 0, 34) // last block, STREAMINFO, length 34
	body := make([]byte, 34)
	// bytes 10..17: rate(20) channels-1(3) bps-1(5) samples(36)
	v := uint64(rate)<<44 | uint64(1)<<41 | uint64(15)<<36 | uint64(samples)&(1<<36-1)
	binary.BigEndian.PutUint64(body[10:], v)
	return append(out, body...)
}
