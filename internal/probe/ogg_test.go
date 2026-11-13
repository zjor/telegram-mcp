package probe

import (
	"bytes"
	"errors"
	"math"
	"testing"

	"github.com/zjor/telegram-mcp/internal/probe/probetest"
)

func probeOgg(t *testing.T, data []byte) (OggInfo, error) {
	t.Helper()
	return Ogg(bytes.NewReader(data), int64(len(data)))
}

func TestOgg_OpusDuration(t *testing.T) {
	info, err := probeOgg(t, probetest.OggFile("opus", 48000, 312, 48000*3+312))
	if err != nil {
		t.Fatal(err)
	}
	if info.Codec != "opus" || math.Abs(info.Duration-3.0) > 0.01 {
		t.Errorf("got %+v", info)
	}
}

func TestOgg_VorbisDuration(t *testing.T) {
	info, err := probeOgg(t, probetest.OggFile("vorbis", 44100, 0, 44100*2))
	if err != nil {
		t.Fatal(err)
	}
	if info.Codec != "vorbis" || math.Abs(info.Duration-2.0) > 0.01 {
		t.Errorf("got %+v", info)
	}
}

func TestOgg_OtherCodec(t *testing.T) {
	info, err := probeOgg(t, probetest.OggFile("speex", 16000, 0, 100))
	if err != nil {
		t.Fatal(err)
	}
	if info.Codec == "opus" || info.Codec == "vorbis" || info.Codec == "" {
		t.Errorf("got %+v", info)
	}
}

func TestOgg_NotOgg(t *testing.T) {
	_, err := probeOgg(t, []byte("ID3\x04\x00\x00\x00\x00\x00\x00"))
	if !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("got %v, want ErrUnknownFormat", err)
	}
}

func TestOgg_Truncated(t *testing.T) {
	data := probetest.OggFile("opus", 48000, 312, 48000)[:10]
	_, err := probeOgg(t, data)
	if err == nil {
		t.Fatal("want error")
	}
}
