package probe

import (
	"bytes"
	"errors"
	"math"
	"testing"

	"github.com/zjor/telegram-mcp/internal/probe/probetest"
)

func probeAudio(b []byte) (AudioInfo, error) {
	return Audio(bytes.NewReader(b), int64(len(b)))
}

func TestAudio(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		format  string
		dur     float64
		wantErr error
	}{
		{"mp3 cbr id3", probetest.MP3File(true, 0, 128, 100), "mp3", 100 * 1152.0 / 44100, nil},
		{"mp3 xing", probetest.MP3File(false, 500, 128, 1), "mp3", 500 * 1152.0 / 44100, nil},
		{"flac", probetest.FLACFile(44100, 44100*4), "flac", 4, nil},
		{"opus", probetest.OggFile("opus", 48000, 312, 48000*3+312), "ogg", 3, nil},
		{"m4a", probetest.MP4File(false, 1000, 7000, probetest.MP4Track{Handler: "soun", Codec: "mp4a"}), "m4a", 7, nil},
		{"mp4 with video", probetest.MP4File(false, 1000, 7000,
			probetest.MP4Track{Handler: "vide", Codec: "avc1", W: 640, H: 360},
			probetest.MP4Track{Handler: "soun", Codec: "mp4a"}), "", 0, ErrUnknownFormat},
		{"theora", probetest.OggFile("theora", 0, 0, 100), "", 0, ErrUnknownFormat},
		{"text", []byte("plain text"), "", 0, ErrUnknownFormat},
		{"text with stray sync", append([]byte("hello \xff\xfb\x90\x00 world"), make([]byte, 50)...), "", 0, ErrUnknownFormat},
		{"empty", nil, "", 0, ErrUnknownFormat},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := probeAudio(tt.data)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got.Format != tt.format || math.Abs(got.Duration-tt.dur) > 0.1 {
				t.Errorf("got %+v, want %s %.3f", got, tt.format, tt.dur)
			}
		})
	}
}

func TestAudio_NegativeSize(t *testing.T) {
	if _, err := Audio(bytes.NewReader([]byte("abcd")), -5); !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("err = %v", err)
	}
	if _, err := MP3(bytes.NewReader(nil), -1); err == nil {
		t.Error("want error")
	}
	if _, err := FLAC(bytes.NewReader(nil), -1); err == nil {
		t.Error("want error")
	}
}

func TestMP3_FLAC_EveryPrefixNoPanic(t *testing.T) {
	files := [][]byte{
		probetest.MP3File(true, 0, 128, 3),
		probetest.MP3File(false, 500, 128, 2),
		probetest.FLACFile(44100, 44100),
	}
	for _, b := range files {
		for n := 0; n <= len(b); n++ {
			_, _ = probeAudio(b[:n])
		}
	}
}
