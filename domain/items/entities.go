package items

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

// Type is an item's own type, fixed at its creation. It is named type on
// the wire because kind is docproc's.
type Type string

const (
	// TypeFile is an item a completed upload created.
	TypeFile Type = "file"
	// TypeRecord is an item a posted JSON record created.
	TypeRecord Type = "record"
)

// Item is one item as the read presents it: a named entry over the blob
// its SHA-256 names. Route and Kind are docproc's route package's output
// for the item's whole content, verbatim; Kind is nil when no signature
// names one. Duration, in seconds, is nil until ingest fills it. The json
// tags are the scan contract: the read's columns carry the same names.
type Item struct {
	ID        string    `json:"id"`
	Type      Type      `json:"type"`
	Name      string    `json:"name"`
	MediaType string    `json:"media_type"`
	SHA256    string    `json:"sha256"`
	Size      int64     `json:"size"`
	Route     string    `json:"route"`
	Kind      *string   `json:"kind"`
	Duration  *float64  `json:"duration"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateFile is the file create command's input: what an upload's
// completion kept of it. UploadID is the upload's id, on which the
// command is idempotent; Name and MediaType are the file name and media
// type its creation carried; SHA256 is its whole content's SHA-256 in
// lowercase hex; and Size is its length in bytes.
type CreateFile struct {
	UploadID  string
	Name      string
	MediaType string
	SHA256    string
	Size      int64
}

var hexSHA256 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Validate rejects an empty upload id, an empty media type, a SHA-256
// that is not 64 lowercase hex digits, and a negative size.
func (c CreateFile) Validate() error {
	var errs []error
	if c.UploadID == "" {
		errs = append(errs, fmt.Errorf("%w: upload id must not be empty", ErrValidation))
	}
	if c.MediaType == "" {
		errs = append(errs, fmt.Errorf("%w: media type must not be empty", ErrValidation))
	}
	if !hexSHA256.MatchString(c.SHA256) {
		errs = append(errs, fmt.Errorf("%w: sha256 must be 64 lowercase hex digits", ErrValidation))
	}
	if c.Size < 0 {
		errs = append(errs, fmt.Errorf("%w: size must not be negative", ErrValidation))
	}
	return errors.Join(errs...)
}
