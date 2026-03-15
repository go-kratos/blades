package blades

import "strings"

// MIMEType represents the media type of content.
type MIMEType string

const (
	// Text and markdown mime types.
	MIMEText     MIMEType = "text/plain"
	MIMEMarkdown MIMEType = "text/markdown"
	// Common image mime types.
	MIMEImagePNG  MIMEType = "image/png"
	MIMEImageJPEG MIMEType = "image/jpeg"
	MIMEImageWEBP MIMEType = "image/webp"
	// Common audio mime types (non-exhaustive).
	MIMEAudioWAV  MIMEType = "audio/wav"
	MIMEAudioMP3  MIMEType = "audio/mpeg"
	MIMEAudioOGG  MIMEType = "audio/ogg"
	MIMEAudioAAC  MIMEType = "audio/aac"
	MIMEAudioFLAC MIMEType = "audio/flac"
	MIMEAudioOpus MIMEType = "audio/opus"
	MIMEAudioPCM  MIMEType = "audio/pcm"
	// Common video mime types (non-exhaustive).
	MIMEVideoMP4 MIMEType = "video/mp4"
	MIMEVideoOGG MIMEType = "video/ogg"
)

// Type returns the general type of the MIMEType (e.g., "image", "audio", "video", or "file").
func (m MIMEType) Type() string {
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

// Format returns the file format associated with the MIMEType.
func (m MIMEType) Format() string {
	parts := strings.SplitN(string(m), "/", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return "octet-stream"
}
