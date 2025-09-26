package blades

import "strings"

// MimeType represents the media type of content.
type MimeType string

const (
	// Text and markdown mime types.
	MimeText     MimeType = "text/plain"
	MimeMarkdown MimeType = "text/markdown"
	// Common image mime types.
	MimeImagePNG  MimeType = "image/png"
	MimeImageJPEG MimeType = "image/jpeg"
	MimeImageWEBP MimeType = "image/webp"
	// Common audio mime types (non-exhaustive).
	MimeAudioWAV  MimeType = "audio/wav"
	MimeAudioMP3  MimeType = "audio/mpeg"
	MimeAudioOGG  MimeType = "audio/ogg"
	MimeAudioAAC  MimeType = "audio/aac"
	MimeAudioFLAC MimeType = "audio/flac"
	MimeAudioOpus MimeType = "audio/opus"
	MimeAudioPCM  MimeType = "audio/pcm"
	// Common video mime types (non-exhaustive).
	MimeVideoMP4 MimeType = "video/mp4"
	MimeVideoOGG MimeType = "video/ogg"
)

// Type returns the general type of the MimeType (e.g., "image", "audio", "video", or "file").
func (m MimeType) Type() string {
	v := string(m)
	switch {
	case strings.HasPrefix(v, "image/"):
		return "image"
	case strings.HasPrefix(v, "audio/"):
		return "audio"
	case strings.HasPrefix(v, "video/"):
		return "video"
	default:
		return "file"
	}
}

// Format returns the file format associated with the MimeType.
func (m MimeType) Format() string {
	parts := strings.SplitN(string(m), "/", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return "octet-stream"
}
