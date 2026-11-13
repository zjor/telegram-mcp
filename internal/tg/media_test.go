package tg

import (
	"errors"
	"reflect"
	"testing"

	tgapi "github.com/gotd/td/tg"
)

func docMedia(mime string, size int64, attrs ...tgapi.DocumentAttributeClass) *tgapi.MessageMediaDocument {
	return &tgapi.MessageMediaDocument{Document: &tgapi.Document{
		ID: 1, AccessHash: 2, FileReference: []byte{3},
		MimeType: mime, Size: size, Attributes: attrs,
	}}
}

func TestMediaFileOf(t *testing.T) {
	tests := []struct {
		name  string
		media tgapi.MessageMediaClass
		want  mediaFile
	}{
		{
			name:  "voice",
			media: docMedia("audio/ogg", 1234, &tgapi.DocumentAttributeAudio{Voice: true, Duration: 7}),
			want: mediaFile{
				Location: &tgapi.InputDocumentFileLocation{ID: 1, AccessHash: 2, FileReference: []byte{3}},
				Kind:     "voice", MimeType: "audio/ogg", Size: 1234, Duration: 7, Ext: ".ogg",
			},
		},
		{
			name:  "named pdf keeps original extension",
			media: docMedia("application/pdf", 10, &tgapi.DocumentAttributeFilename{FileName: "a.PDF"}),
			want: mediaFile{
				Location: &tgapi.InputDocumentFileLocation{ID: 1, AccessHash: 2, FileReference: []byte{3}},
				Kind:     "document", MimeType: "application/pdf", Size: 10, FileName: "a.PDF", Ext: ".PDF",
			},
		},
		{
			name:  "unsafe file name falls back to mime",
			media: docMedia("application/octet-stream", 10, &tgapi.DocumentAttributeFilename{FileName: "../../etc"}),
			want: mediaFile{
				Location: &tgapi.InputDocumentFileLocation{ID: 1, AccessHash: 2, FileReference: []byte{3}},
				Kind:     "document", MimeType: "application/octet-stream", Size: 10, FileName: "../../etc", Ext: ".bin",
			},
		},
		{
			name:  "video note",
			media: docMedia("video/mp4", 10, &tgapi.DocumentAttributeVideo{RoundMessage: true, Duration: 3}),
			want: mediaFile{
				Location: &tgapi.InputDocumentFileLocation{ID: 1, AccessHash: 2, FileReference: []byte{3}},
				Kind:     "video_note", MimeType: "video/mp4", Size: 10, Duration: 3, Ext: ".mp4",
			},
		},
		{
			name:  "sticker",
			media: docMedia("image/webp", 10, &tgapi.DocumentAttributeSticker{}),
			want: mediaFile{
				Location: &tgapi.InputDocumentFileLocation{ID: 1, AccessHash: 2, FileReference: []byte{3}},
				Kind:     "sticker", MimeType: "image/webp", Size: 10, Ext: ".webp",
			},
		},
		{
			name: "photo takes largest size",
			media: &tgapi.MessageMediaPhoto{Photo: &tgapi.Photo{
				ID: 5, AccessHash: 6, FileReference: []byte{7},
				Sizes: []tgapi.PhotoSizeClass{
					&tgapi.PhotoStrippedSize{Type: "i"},
					&tgapi.PhotoSize{Type: "m", W: 320, H: 240, Size: 100},
					&tgapi.PhotoSizeProgressive{Type: "y", W: 1280, H: 960, Sizes: []int{10, 20, 300}},
				},
			}},
			want: mediaFile{
				Location: &tgapi.InputPhotoFileLocation{ID: 5, AccessHash: 6, FileReference: []byte{7}, ThumbSize: "y"},
				Kind:     "photo", MimeType: "image/jpeg", Size: 300, Ext: ".jpg",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mediaFileOf(tt.media)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got  %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestMediaFileOf_Errors(t *testing.T) {
	tests := []struct {
		name  string
		media tgapi.MessageMediaClass
		want  error
	}{
		{"nil", nil, ErrNoMedia},
		{"geo", &tgapi.MessageMediaGeo{}, ErrNotDownloadable},
		{"poll", &tgapi.MessageMediaPoll{}, ErrNotDownloadable},
		{"contact", &tgapi.MessageMediaContact{}, ErrNotDownloadable},
		{"webpage", &tgapi.MessageMediaWebPage{}, ErrNotDownloadable},
		{"empty document", &tgapi.MessageMediaDocument{Document: &tgapi.DocumentEmpty{}}, ErrNotDownloadable},
		{"no document", &tgapi.MessageMediaDocument{}, ErrNotDownloadable},
		{"empty photo", &tgapi.MessageMediaPhoto{Photo: &tgapi.PhotoEmpty{}}, ErrNotDownloadable},
		{"photo with only thumbs", &tgapi.MessageMediaPhoto{Photo: &tgapi.Photo{Sizes: []tgapi.PhotoSizeClass{
			&tgapi.PhotoStrippedSize{Type: "i"}, &tgapi.PhotoCachedSize{Type: "s"},
		}}}, ErrNotDownloadable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := mediaFileOf(tt.media); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}
