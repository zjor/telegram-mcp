package probe

import (
	"errors"
	"io"
)

// AudioInfo describes an audio file Telegram can send as audio.
type AudioInfo struct {
	Format   string  // "mp3", "m4a", "ogg" or "flac"
	Duration float64 // seconds; 0 if unknown
}

// Audio detects the audio format by content. A valid OGG or MP4 container
// with an unsupported codec or track layout gives ErrUnknownFormat; it is
// not tried as MP3. MP3 is tried last because its sync search is the loosest.
func Audio(r io.ReaderAt, size int64) (AudioInfo, error) {
	if size < 4 {
		return AudioInfo{}, ErrUnknownFormat
	}
	var head [12]byte
	n, _ := r.ReadAt(head[:], 0)
	h := head[:n]

	switch {
	case len(h) >= 4 && string(h[:4]) == "OggS":
		o, err := Ogg(r, size)
		if err != nil {
			return AudioInfo{}, err
		}
		if o.Codec != "opus" && o.Codec != "vorbis" {
			return AudioInfo{}, ErrUnknownFormat
		}
		return AudioInfo{Format: "ogg", Duration: o.Duration}, nil
	case len(h) >= 8 && string(h[4:8]) == "ftyp":
		m, err := MP4(r, size)
		if err != nil {
			return AudioInfo{}, err
		}
		if m.AudioCodec != "mp4a" || m.VideoCodec != "" {
			return AudioInfo{}, ErrUnknownFormat
		}
		return AudioInfo{Format: "m4a", Duration: m.Duration}, nil
	case len(h) >= 4 && string(h[:4]) == "fLaC":
		d, err := FLAC(r, size)
		if err != nil {
			return AudioInfo{}, err
		}
		return AudioInfo{Format: "flac", Duration: d}, nil
	}
	d, err := MP3(r, size)
	if err != nil {
		if errors.Is(err, ErrUnknownFormat) {
			return AudioInfo{}, ErrUnknownFormat
		}
		return AudioInfo{}, err
	}
	return AudioInfo{Format: "mp3", Duration: d}, nil
}
