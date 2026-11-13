package tg

import (
	"bytes"
	"testing"

	tgapi "github.com/gotd/td/tg"
	"github.com/zjor/telegram-mcp/internal/probe/probetest"
)

func TestParseFileKind(t *testing.T) {
	for _, s := range []string{"photo", "document", "audio", "voice", "video", "video_note"} {
		k, err := ParseFileKind(s)
		if err != nil || string(k) != s {
			t.Errorf("ParseFileKind(%q) = %q, %v", s, k, err)
		}
	}
	for _, s := range []string{"Photo", "", "gif"} {
		_, err := ParseFileKind(s)
		if err == nil || err.Error() != "kind must be one of photo, document, audio, voice, video, video_note" {
			t.Errorf("ParseFileKind(%q) err = %v", s, err)
		}
	}
}

func mp4v(w, h int, codec string, withAudio bool) []byte {
	tr := []probetest.MP4Track{{Handler: "vide", Codec: codec, W: w, H: h}}
	if withAudio {
		tr = append(tr, probetest.MP4Track{Handler: "soun", Codec: "mp4a"})
	}
	return probetest.MP4File(false, 1000, 10000, tr...)
}

const cropMsg = `video_note must be square, got 640x480; crop with: ffmpeg -i in.mp4 -vf "crop='min(iw,ih)':'min(iw,ih)',scale=384:384" -c:v libx264 -c:a aac out.mp4`

func TestBuildMedia(t *testing.T) {
	fn := func(n string) tgapi.DocumentAttributeClass { return &tgapi.DocumentAttributeFilename{FileName: n} }
	png := append([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, make([]byte, 16)...)
	opus3 := probetest.OggFile("opus", 48000, 312, 3*48000+312)
	tests := []struct {
		name  string
		kind  FileKind
		data  []byte
		fname string
		want  mediaSpec
		err   string
	}{
		{name: "photo", kind: KindPhoto, data: png, fname: "a.png", want: mediaSpec{Photo: true}},
		{name: "photo bad", kind: KindPhoto, data: []byte("hello"), fname: "a.txt", err: "photo needs JPEG, PNG or WebP; send it as document instead"},
		{name: "doc txt", kind: KindDocument, data: []byte("hello"), fname: "notes.txt",
			want: mediaSpec{MimeType: "text/plain", ForceFile: true, Attributes: []tgapi.DocumentAttributeClass{fn("notes.txt")}}},
		{name: "doc blob", kind: KindDocument, data: []byte("hello"), fname: "blob",
			want: mediaSpec{MimeType: "application/octet-stream", ForceFile: true, Attributes: []tgapi.DocumentAttributeClass{fn("blob")}}},
		{name: "audio bad", kind: KindAudio, data: []byte("hello"), fname: "x", err: "audio needs MP3, M4A, OGG or FLAC; send it as document instead"},
		{name: "voice ok", kind: KindVoice, data: opus3, fname: "v.ogg",
			want: mediaSpec{MimeType: "audio/ogg", Attributes: []tgapi.DocumentAttributeClass{&tgapi.DocumentAttributeAudio{Voice: true, Duration: 3}}}},
		{name: "voice vorbis", kind: KindVoice, data: probetest.OggFile("vorbis", 44100, 0, 44100), fname: "v.ogg",
			err: "voice needs OGG/Opus; convert with: ffmpeg -i <in> -c:a libopus -b:a 32k out.ogg"},
		{name: "video ok", kind: KindVideo, data: mp4v(1920, 1080, "avc1", true), fname: "m.mp4",
			want: mediaSpec{MimeType: "video/mp4", Attributes: []tgapi.DocumentAttributeClass{
				&tgapi.DocumentAttributeVideo{SupportsStreaming: true, Duration: 10, W: 1920, H: 1080}, fn("m.mp4")}}},
		{name: "video audio only", kind: KindVideo, data: probetest.MP4File(false, 1000, 10000, probetest.MP4Track{Handler: "soun", Codec: "mp4a"}), fname: "m.mp4",
			err: "video needs MP4 with H.264; convert with: ffmpeg -i <in> -c:v libx264 -c:a aac out.mp4"},
		{name: "note audio only", kind: KindVideoNote, data: probetest.MP4File(false, 1000, 10000, probetest.MP4Track{Handler: "soun", Codec: "mp4a"}), fname: "m.mp4",
			err: "video_note needs MP4 with H.264; convert with: ffmpeg -i <in> -c:v libx264 -c:a aac out.mp4"},
		{name: "note hvc1", kind: KindVideoNote, data: mp4v(384, 384, "hvc1", false), fname: "m.mp4",
			err: "video_note needs MP4 with H.264; convert with: ffmpeg -i <in> -c:v libx264 -c:a aac out.mp4"},
		{name: "note ok", kind: KindVideoNote, data: mp4v(384, 384, "avc1", true), fname: "m.mp4",
			want: mediaSpec{MimeType: "video/mp4", Attributes: []tgapi.DocumentAttributeClass{
				&tgapi.DocumentAttributeVideo{RoundMessage: true, Duration: 10, W: 384, H: 384}}}},
		{name: "note not square", kind: KindVideoNote, data: mp4v(640, 480, "avc1", false), fname: "m.mp4", err: cropMsg},
		{name: "note 61s", kind: KindVideoNote, data: probetest.MP4File(false, 1000, 61000, probetest.MP4Track{Handler: "vide", Codec: "avc1", W: 384, H: 384}), fname: "m.mp4",
			err: "video_note is 61 s, limit is 60 s"},
		{name: "note fragmented 90s", kind: KindVideoNote, data: probetest.MP4FileFragmented(1000, 90000, true, probetest.MP4Track{Handler: "vide", Codec: "avc1", W: 384, H: 384}), fname: "m.mp4",
			err: "video_note is 90 s, limit is 60 s"},
		{name: "video duration 0", kind: KindVideo, data: probetest.MP4FileFragmented(1000, 0, false, probetest.MP4Track{Handler: "vide", Codec: "avc1", W: 384, H: 384}), fname: "m.mp4",
			err: "video: cannot read duration; remux with: ffmpeg -i <in> -c copy -movflags +faststart out.mp4"},
		{name: "note duration 0", kind: KindVideoNote, data: probetest.MP4FileFragmented(1000, 0, false, probetest.MP4Track{Handler: "vide", Codec: "avc1", W: 384, H: 384}), fname: "m.mp4",
			err: "video_note: cannot read duration; remux with: ffmpeg -i <in> -c copy -movflags +faststart out.mp4"},
		{name: "note 60.4s", kind: KindVideoNote, data: probetest.MP4File(false, 1000, 60400, probetest.MP4Track{Handler: "vide", Codec: "avc1", W: 384, H: 384}), fname: "m.mp4",
			want: mediaSpec{MimeType: "video/mp4", Attributes: []tgapi.DocumentAttributeClass{
				&tgapi.DocumentAttributeVideo{RoundMessage: true, Duration: 60.4, W: 384, H: 384}}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildMedia(tc.kind, bytes.NewReader(tc.data), int64(len(tc.data)), tc.fname)
			if tc.err != "" {
				if err == nil || err.Error() != tc.err {
					t.Fatalf("err = %v, want %q", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Photo != tc.want.Photo || got.MimeType != tc.want.MimeType || got.ForceFile != tc.want.ForceFile {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
			if len(got.Attributes) != len(tc.want.Attributes) {
				t.Fatalf("attrs %v, want %v", got.Attributes, tc.want.Attributes)
			}
			for i, a := range got.Attributes {
				if a.TypeID() != tc.want.Attributes[i].TypeID() {
					t.Fatalf("attr %d = %T, want %T", i, a, tc.want.Attributes[i])
				}
				if v, ok := a.(*tgapi.DocumentAttributeVideo); ok {
					w := tc.want.Attributes[i].(*tgapi.DocumentAttributeVideo)
					if *v != *w {
						t.Fatalf("video attr %+v, want %+v", v, w)
					}
				}
				if v, ok := a.(*tgapi.DocumentAttributeAudio); ok {
					w := tc.want.Attributes[i].(*tgapi.DocumentAttributeAudio)
					if v.Voice != w.Voice || v.Duration != w.Duration {
						t.Fatalf("audio attr %+v, want %+v", v, w)
					}
				}
				if v, ok := a.(*tgapi.DocumentAttributeFilename); ok {
					if w := tc.want.Attributes[i].(*tgapi.DocumentAttributeFilename); v.FileName != w.FileName {
						t.Fatalf("filename %q, want %q", v.FileName, w.FileName)
					}
				}
			}
		})
	}
}

func TestBuildMediaAudio(t *testing.T) {
	data := probetest.MP3File(false, 0, 128, 200)
	got, err := buildMedia(KindAudio, bytes.NewReader(data), int64(len(data)), "song.mp3")
	if err != nil {
		t.Fatal(err)
	}
	if got.MimeType != "audio/mpeg" || len(got.Attributes) != 2 {
		t.Fatalf("got %+v", got)
	}
	a, ok := got.Attributes[0].(*tgapi.DocumentAttributeAudio)
	if !ok || a.Duration <= 0 || a.Voice {
		t.Fatalf("audio attr %+v", got.Attributes[0])
	}
	if f, ok := got.Attributes[1].(*tgapi.DocumentAttributeFilename); !ok || f.FileName != "song.mp3" {
		t.Fatalf("filename attr %+v", got.Attributes[1])
	}
}
