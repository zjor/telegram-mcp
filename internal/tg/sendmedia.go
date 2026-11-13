package tg

import (
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"path/filepath"

	"github.com/zjor/telegram-mcp/internal/probe"

	tgapi "github.com/gotd/td/tg"
)

// mediaSpec holds the media fields that depend on the file kind.
type mediaSpec struct {
	Photo      bool
	MimeType   string
	Attributes []tgapi.DocumentAttributeClass
	ForceFile  bool
}

const (
	errPhoto     = "photo needs JPEG, PNG or WebP; send it as document instead"
	errAudio     = "audio needs MP3, M4A, OGG or FLAC; send it as document instead"
	errVoice     = "voice needs OGG/Opus; convert with: ffmpeg -i <in> -c:a libopus -b:a 32k out.ogg"
	errVideoFmt  = "needs MP4 with H.264; convert with: ffmpeg -i <in> -c:v libx264 -c:a aac out.mp4"
	videoNoteMax = 60
)

// buildMedia checks that the file fits the kind and returns the media fields.
// name is the base name of the resolved path. Any probe failure gives the
// kind's error text, so the caller sees how to fix the file.
func buildMedia(kind FileKind, r io.ReaderAt, size int64, name string) (mediaSpec, error) {
	filename := &tgapi.DocumentAttributeFilename{FileName: name}
	switch kind {
	case KindPhoto:
		if _, err := probe.ImageFormat(r); err != nil {
			return mediaSpec{}, errors.New(errPhoto)
		}
		return mediaSpec{Photo: true}, nil

	case KindDocument:
		mt := "application/octet-stream"
		if t := mime.TypeByExtension(filepath.Ext(name)); t != "" {
			if m, _, err := mime.ParseMediaType(t); err == nil {
				mt = m
			}
		}
		return mediaSpec{MimeType: mt, ForceFile: true, Attributes: []tgapi.DocumentAttributeClass{filename}}, nil

	case KindAudio:
		info, err := probe.Audio(r, size)
		mt := map[string]string{"mp3": "audio/mpeg", "m4a": "audio/mp4", "ogg": "audio/ogg", "flac": "audio/flac"}[info.Format]
		if err != nil || mt == "" {
			return mediaSpec{}, errors.New(errAudio)
		}
		return mediaSpec{MimeType: mt, Attributes: []tgapi.DocumentAttributeClass{
			&tgapi.DocumentAttributeAudio{Duration: int(math.Round(info.Duration))}, filename}}, nil

	case KindVoice:
		info, err := probe.Ogg(r, size)
		if err != nil || info.Codec != "opus" {
			return mediaSpec{}, errors.New(errVoice)
		}
		return mediaSpec{MimeType: "audio/ogg", Attributes: []tgapi.DocumentAttributeClass{
			&tgapi.DocumentAttributeAudio{Voice: true, Duration: max(1, int(math.Round(info.Duration)))}}}, nil

	case KindVideo, KindVideoNote:
		info, err := probe.MP4(r, size)
		okVideo := info.VideoCodec == "avc1" || info.VideoCodec == "avc3"
		okAudio := info.AudioCodec == "" || info.AudioCodec == "mp4a"
		if err != nil || !okVideo || !okAudio {
			return mediaSpec{}, fmt.Errorf("%s %s", kind, errVideoFmt)
		}
		if !(info.Duration > 0) {
			return mediaSpec{}, fmt.Errorf("%s: cannot read duration; remux with: ffmpeg -i <in> -c copy -movflags +faststart out.mp4", kind)
		}
		attr := &tgapi.DocumentAttributeVideo{Duration: info.Duration, W: info.Width, H: info.Height}
		if kind == KindVideo {
			attr.SupportsStreaming = true
			return mediaSpec{MimeType: "video/mp4", Attributes: []tgapi.DocumentAttributeClass{attr, filename}}, nil
		}
		if info.Width != info.Height {
			return mediaSpec{}, fmt.Errorf(`video_note must be square, got %dx%d; crop with: ffmpeg -i in.mp4 -vf "crop='min(iw,ih)':'min(iw,ih)',scale=384:384" -c:v libx264 -c:a aac out.mp4`, info.Width, info.Height)
		}
		if n := int(math.Round(info.Duration)); n > videoNoteMax {
			return mediaSpec{}, fmt.Errorf("video_note is %d s, limit is %d s", n, videoNoteMax)
		}
		attr.RoundMessage = true
		return mediaSpec{MimeType: "video/mp4", Attributes: []tgapi.DocumentAttributeClass{attr}}, nil
	}
	return mediaSpec{}, errors.New("kind must be one of photo, document, audio, voice, video, video_note")
}
