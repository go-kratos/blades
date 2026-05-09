package content

// FilePart represents binary content referenced by URI.
type FilePart struct {
	URI      string
	MIME     string
	Filename string
}

// FileRefPart represents binary content referenced by provider-managed file ID.
type FileRefPart struct {
	ID   string
	MIME string
}

// DataPart represents binary content embedded inline.
type DataPart struct {
	Bytes    []byte
	MIME     string
	Filename string
}

func (FilePart) part()    {}
func (FileRefPart) part() {}
func (DataPart) part()    {}
