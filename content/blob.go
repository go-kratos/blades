package content

// BlobSource is the sealed interface for blob data sources.
type BlobSource interface {
	blobSource()
}

// InlineBytes represents raw byte content embedded inline.
type InlineBytes []byte

func (InlineBytes) blobSource() {}

// URI represents a reference to content by URL.
type URI string

func (URI) blobSource() {}

// FileID represents a reference to content by file identifier.
type FileID string

func (FileID) blobSource() {}

// Blob is a binary content part with MIME type and source.
type Blob struct {
	MIME   string
	Source BlobSource
}

func (Blob) part() {}
