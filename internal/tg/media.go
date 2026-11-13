package tg

import (
	"errors"
	"mime"
	"path/filepath"
	"regexp"

	tgapi "github.com/gotd/td/tg"
)

var (
	ErrNoMedia         = errors.New("message has no media")
	ErrNotDownloadable = errors.New("media cannot be downloaded")
)

// mediaFile describes the downloadable file of one message.
type mediaFile struct {
	Location tgapi.InputFileLocationClass
	Kind     string
	MimeType string
	Size     int64
	FileName string // original document name, "" if none
	Duration int    // seconds; 0 if unknown
	Ext      string // with leading dot
}

func mediaFileOf(media tgapi.MessageMediaClass) (mediaFile, error) {
	switch md := media.(type) {
	case nil:
		return mediaFile{}, ErrNoMedia
	case *tgapi.MessageMediaPhoto:
		return photoFile(md)
	case *tgapi.MessageMediaDocument:
		return documentFile(md)
	}
	return mediaFile{}, ErrNotDownloadable
}

func photoFile(md *tgapi.MessageMediaPhoto) (mediaFile, error) {
	photo, ok := md.Photo.(*tgapi.Photo)
	if !ok {
		return mediaFile{}, ErrNotDownloadable
	}
	var (
		bestType string
		bestArea int
		bestSize int
	)
	for _, sc := range photo.Sizes {
		var (
			typ        string
			area, size int
		)
		switch s := sc.(type) {
		case *tgapi.PhotoSize:
			typ, area, size = s.Type, s.W*s.H, s.Size
		case *tgapi.PhotoSizeProgressive:
			if len(s.Sizes) == 0 {
				continue
			}
			typ, area, size = s.Type, s.W*s.H, s.Sizes[len(s.Sizes)-1]
		default: // stripped, cached and path thumbs are not real files
			continue
		}
		if bestType == "" || area > bestArea {
			bestType, bestArea, bestSize = typ, area, size
		}
	}
	if bestType == "" {
		return mediaFile{}, ErrNotDownloadable
	}
	return mediaFile{
		Location: &tgapi.InputPhotoFileLocation{
			ID:            photo.ID,
			AccessHash:    photo.AccessHash,
			FileReference: photo.FileReference,
			ThumbSize:     bestType,
		},
		Kind:     "photo",
		MimeType: "image/jpeg",
		Size:     int64(bestSize),
		Ext:      ".jpg",
	}, nil
}

func documentFile(md *tgapi.MessageMediaDocument) (mediaFile, error) {
	doc, ok := md.Document.(*tgapi.Document)
	if !ok {
		return mediaFile{}, ErrNotDownloadable
	}
	kind, name, duration := documentKind(doc)
	return mediaFile{
		Location: &tgapi.InputDocumentFileLocation{
			ID:            doc.ID,
			AccessHash:    doc.AccessHash,
			FileReference: doc.FileReference,
		},
		Kind:     kind,
		MimeType: doc.MimeType,
		Size:     doc.Size,
		FileName: name,
		Duration: duration,
		Ext:      extension(name, doc.MimeType),
	}, nil
}

// documentKind classifies a document as sticker, voice, audio, video_note,
// video, gif or document. read_chat placeholders use the same kinds.
func documentKind(doc *tgapi.Document) (kind, fileName string, duration int) {
	var sticker, voice, audio, videoNote, video, animated bool
	for _, a := range doc.Attributes {
		switch a := a.(type) {
		case *tgapi.DocumentAttributeSticker:
			sticker = true
		case *tgapi.DocumentAttributeAudio:
			voice, audio = a.Voice, !a.Voice
			duration = a.Duration
		case *tgapi.DocumentAttributeVideo:
			videoNote, video = a.RoundMessage, !a.RoundMessage
			if duration == 0 {
				duration = int(a.Duration)
			}
		case *tgapi.DocumentAttributeAnimated:
			animated = true
		case *tgapi.DocumentAttributeFilename:
			fileName = a.FileName
		}
	}
	switch {
	case sticker:
		kind = "sticker"
	case voice:
		kind = "voice"
	case audio:
		kind = "audio"
	case videoNote:
		kind = "video_note"
	case video:
		kind = "video"
	case animated:
		kind = "gif"
	default:
		kind = "document"
	}
	return kind, fileName, duration
}

var safeExt = regexp.MustCompile(`^\.[A-Za-z0-9]{1,10}$`)

var mimeExt = map[string]string{
	"audio/ogg":               ".ogg",
	"audio/mpeg":              ".mp3",
	"audio/mp4":               ".m4a",
	"video/mp4":               ".mp4",
	"video/webm":              ".webm",
	"image/jpeg":              ".jpg",
	"image/png":               ".png",
	"image/webp":              ".webp",
	"application/x-tgsticker": ".tgs",
	"application/pdf":         ".pdf",
}

// extension prefers the original file name's suffix when it is a short plain
// one, then a fixed MIME table, then the system MIME table, then ".bin".
func extension(fileName, mimeType string) string {
	if ext := filepath.Ext(fileName); safeExt.MatchString(ext) {
		return ext
	}
	if ext, ok := mimeExt[mimeType]; ok {
		return ext
	}
	if exts, _ := mime.ExtensionsByType(mimeType); len(exts) > 0 && safeExt.MatchString(exts[0]) {
		return exts[0]
	}
	return ".bin"
}
