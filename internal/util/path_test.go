package util

import "testing"

// Tests PNG/JPEG magic-byte detection and non-image rejection.
func TestSniffImageType_PngJpegMagicAndUnknown(t *testing.T) {
	cases := []struct {
		name      string
		data      []byte
		wantMedia string
		wantExt   string
	}{
		{
			name:      "png magic",
			data:      append([]byte("\x89PNG\r\n\x1a\n"), []byte("rest")...),
			wantMedia: "image/png",
			wantExt:   ".png",
		},
		{
			name:      "jpeg magic",
			data:      append([]byte("\xff\xd8\xff\xe0"), []byte("rest")...),
			wantMedia: "image/jpeg",
			wantExt:   ".jpg",
		},
		{
			name:      "plain text",
			data:      []byte("hello clipboard"),
			wantMedia: "",
			wantExt:   "",
		},
		{
			name:      "empty",
			data:      nil,
			wantMedia: "",
			wantExt:   "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			media, ext := SniffImageType(tc.data)
			if media != tc.wantMedia || ext != tc.wantExt {
				t.Errorf(
					"want (%q, %q), got (%q, %q)",
					tc.wantMedia, tc.wantExt, media, ext,
				)
			}
		})
	}
}
