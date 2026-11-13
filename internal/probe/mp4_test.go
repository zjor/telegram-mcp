package probe

import (
	"bytes"
	"errors"
	"testing"

	"github.com/zjor/telegram-mcp/internal/probe/probetest"
)

func probeMP4(t *testing.T, b []byte) (MP4Info, error) {
	t.Helper()
	return MP4(bytes.NewReader(b), int64(len(b)))
}

func squareFile(moovLast bool) []byte {
	return probetest.MP4File(moovLast, 1000, 5000,
		probetest.MP4Track{Handler: "vide", Codec: "avc1", W: 384, H: 384},
		probetest.MP4Track{Handler: "soun", Codec: "mp4a"})
}

func TestMP4_SquareVideoWithAudio(t *testing.T) {
	got, err := probeMP4(t, squareFile(false))
	if err != nil {
		t.Fatal(err)
	}
	want := MP4Info{VideoCodec: "avc1", AudioCodec: "mp4a", Width: 384, Height: 384, Duration: 5.0}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestMP4_MoovAfterMdat(t *testing.T) {
	got, err := probeMP4(t, squareFile(true))
	if err != nil {
		t.Fatal(err)
	}
	want := MP4Info{VideoCodec: "avc1", AudioCodec: "mp4a", Width: 384, Height: 384, Duration: 5.0}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestMP4_Rotated(t *testing.T) {
	b := probetest.MP4File(false, 1000, 1000,
		probetest.MP4Track{Handler: "vide", Codec: "avc1", W: 1920, H: 1080, Rotate90: true})
	got, err := probeMP4(t, b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Width != 1080 || got.Height != 1920 {
		t.Errorf("size %dx%d, want 1080x1920", got.Width, got.Height)
	}
}

func TestMP4_AudioOnly(t *testing.T) {
	b := probetest.MP4File(false, 1000, 2000, probetest.MP4Track{Handler: "soun", Codec: "mp4a"})
	got, err := probeMP4(t, b)
	if err != nil {
		t.Fatal(err)
	}
	if got.VideoCodec != "" || got.AudioCodec != "mp4a" {
		t.Errorf("got %+v", got)
	}
}

func TestMP4_HEVC(t *testing.T) {
	b := probetest.MP4File(false, 1000, 1000,
		probetest.MP4Track{Handler: "vide", Codec: "hvc1", W: 640, H: 640})
	got, err := probeMP4(t, b)
	if err != nil {
		t.Fatal(err)
	}
	if got.VideoCodec != "hvc1" {
		t.Errorf("VideoCodec %q, want hvc1", got.VideoCodec)
	}
}

func TestMP4_NotMP4(t *testing.T) {
	_, err := probeMP4(t, probetest.OggFile("opus", 48000, 0, 48000))
	if !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("err = %v, want ErrUnknownFormat", err)
	}
}

func TestMP4_Truncated(t *testing.T) {
	b := squareFile(false)
	_, err := probeMP4(t, b[:len(b)-40])
	if err == nil {
		t.Fatal("want error for truncated moov")
	}
}

func TestMP4_EveryPrefixNoPanic(t *testing.T) {
	for _, moovLast := range []bool{false, true} {
		b := squareFile(moovLast)
		for n := 0; n <= len(b); n++ {
			_, _ = probeMP4(t, b[:n])
		}
	}
}

func TestMP4_BadBoxSizes(t *testing.T) {
	// Box size smaller than its header must not loop forever.
	bad := []byte{0, 0, 0, 4, 'f', 't', 'y', 'p', 0, 0, 0, 8, 'm', 'o', 'o', 'v'}
	if _, err := probeMP4(t, bad); err == nil {
		t.Error("want error for undersized box")
	}
	// Huge 64-bit size.
	huge := []byte{0, 0, 0, 1, 'm', 'o', 'o', 'v', 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	if _, err := probeMP4(t, huge); err == nil {
		t.Error("want error for huge box")
	}
}

func TestMP4_FragmentedMehd(t *testing.T) {
	b := probetest.MP4FileFragmented(1000, 90000, true,
		probetest.MP4Track{Handler: "vide", Codec: "avc1", W: 384, H: 384})
	got, err := probeMP4(t, b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Duration != 90 {
		t.Errorf("Duration = %v, want 90", got.Duration)
	}
}

func TestMP4_FragmentedNoMehd(t *testing.T) {
	b := probetest.MP4FileFragmented(1000, 0, false,
		probetest.MP4Track{Handler: "vide", Codec: "avc1", W: 384, H: 384})
	got, err := probeMP4(t, b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Duration != 0 || got.VideoCodec != "avc1" {
		t.Errorf("got %+v, want Duration 0", got)
	}
}
