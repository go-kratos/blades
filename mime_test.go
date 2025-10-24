package blades

import (
	"testing"
)

func TestMIMEType(t *testing.T) {
	tests := []struct {
		name     string
		mimeType MIMEType
		expected string
	}{
		{"Text MIME", MIMEText, "text/plain"},
		{"Markdown MIME", MIMEMarkdown, "text/markdown"},
		{"PNG MIME", MIMEImagePNG, "image/png"},
		{"JPEG MIME", MIMEImageJPEG, "image/jpeg"},
		{"WEBP MIME", MIMEImageWEBP, "image/webp"},
		{"WAV MIME", MIMEAudioWAV, "audio/wav"},
		{"MP3 MIME", MIMEAudioMP3, "audio/mpeg"},
		{"OGG MIME", MIMEAudioOGG, "audio/ogg"},
		{"AAC MIME", MIMEAudioAAC, "audio/aac"},
		{"FLAC MIME", MIMEAudioFLAC, "audio/flac"},
		{"Opus MIME", MIMEAudioOpus, "audio/opus"},
		{"PCM MIME", MIMEAudioPCM, "audio/pcm"},
		{"MP4 MIME", MIMEVideoMP4, "video/mp4"},
		{"OGG Video MIME", MIMEVideoOGG, "video/ogg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.mimeType) != tt.expected {
				t.Errorf("MIMEType = %v, want %v", tt.mimeType, tt.expected)
			}
		})
	}
}

func TestMIMETypeType(t *testing.T) {
	tests := []struct {
		name     string
		mimeType MIMEType
		expected string
	}{
		{"Text type", MIMEText, "file"},
		{"Markdown type", MIMEMarkdown, "file"},
		{"PNG type", MIMEImagePNG, "image"},
		{"JPEG type", MIMEImageJPEG, "image"},
		{"WEBP type", MIMEImageWEBP, "image"},
		{"WAV type", MIMEAudioWAV, "audio"},
		{"MP3 type", MIMEAudioMP3, "audio"},
		{"OGG audio type", MIMEAudioOGG, "audio"},
		{"AAC type", MIMEAudioAAC, "audio"},
		{"FLAC type", MIMEAudioFLAC, "audio"},
		{"Opus type", MIMEAudioOpus, "audio"},
		{"PCM type", MIMEAudioPCM, "audio"},
		{"MP4 type", MIMEVideoMP4, "video"},
		{"OGG video type", MIMEVideoOGG, "video"},
		{"Custom image type", MIMEType("image/gif"), "image"},
		{"Custom audio type", MIMEType("audio/mp4"), "audio"},
		{"Custom video type", MIMEType("video/avi"), "video"},
		{"Custom file type", MIMEType("application/pdf"), "file"},
		{"Unknown type", MIMEType("unknown/type"), "file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.mimeType.Type()
			if result != tt.expected {
				t.Errorf("MIMEType.Type() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestMIMETypeFormat(t *testing.T) {
	tests := []struct {
		name     string
		mimeType MIMEType
		expected string
	}{
		{"Text format", MIMEText, "plain"},
		{"Markdown format", MIMEMarkdown, "markdown"},
		{"PNG format", MIMEImagePNG, "png"},
		{"JPEG format", MIMEImageJPEG, "jpeg"},
		{"WEBP format", MIMEImageWEBP, "webp"},
		{"WAV format", MIMEAudioWAV, "wav"},
		{"MP3 format", MIMEAudioMP3, "mpeg"},
		{"OGG audio format", MIMEAudioOGG, "ogg"},
		{"AAC format", MIMEAudioAAC, "aac"},
		{"FLAC format", MIMEAudioFLAC, "flac"},
		{"Opus format", MIMEAudioOpus, "opus"},
		{"PCM format", MIMEAudioPCM, "pcm"},
		{"MP4 format", MIMEVideoMP4, "mp4"},
		{"OGG video format", MIMEVideoOGG, "ogg"},
		{"Custom format", MIMEType("image/gif"), "gif"},
		{"Invalid format", MIMEType("invalid"), "octet-stream"},
		{"Empty format", MIMEType(""), "octet-stream"},
		{"Single slash", MIMEType("text"), "octet-stream"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.mimeType.Format()
			if result != tt.expected {
				t.Errorf("MIMEType.Format() = %v, want %v", result, tt.expected)
			}
		})
	}
}
