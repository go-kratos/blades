package blades

// MimeType represents the media type of content.
type MimeType string

const (
	// Text and markdown mime types.
	MimeText     MimeType = "text/plain"
	MimeMarkdown MimeType = "text/markdown"
	// Common image mime types.
	MimeImagePNG  MimeType = "image/png"
	MimeImageJPEG MimeType = "image/jpeg"
	// Common audio mime types (non-exhaustive).
	MimeAudioWAV MimeType = "audio/wav"
	MimeAudioMP3 MimeType = "audio/mpeg"
	MimeAudioOGG MimeType = "audio/ogg"
)
