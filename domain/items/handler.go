package items

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"

	"github.com/standards-lab/go-database"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/upload"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
)

type handler struct {
	service *Service
	limits  web.Limits
	records web.Transfer
	limit   int64
}

// Routes builds the layer's route group, /items: the record create, the
// read by id, and the paged read of every item, under limits. The record
// create is a POST of a JSON body named by its Content-Disposition file
// name, read under records, whose limit bounds the body and whose rate
// sets the connection's deadlines; it answers 201 with the item and a
// Location of the request's path, then the item's id. logger records the
// cause of every 5xx the group's error writer sends. Every refusal is a
// problem response: a malformed id, a sort or filter, a cursor the read
// did not issue, a record with no name or a body that is not JSON a 400,
// an unknown item a 404, a body slower than the rate a 408, a body over
// the limit a 413, and an unreachable database a 503. A zero records
// Transfer is a wiring defect and panics.
func Routes(service *Service, limits web.Limits, records web.Transfer, logger *slog.Logger) *web.Group {
	h := &handler{service: service, limits: limits, records: records, limit: records.Limit()}
	g := web.NewGroup("/items")
	g.SetErrorWriter(web.NewErrorWriter(logger, status))
	g.HandleErr("POST", "", h.create)
	g.HandleErr("GET", "", h.list)
	g.HandleErr("GET", "/{id}", h.find)
	return g
}

// Completion returns the Complete function of go-web-sdk's upload
// handler: it creates the file item for the completed upload through
// [Service.CreateFile] and answers 201 with the item and a Location of
// base, the path the /items group is mounted at, then the item's id. A
// repeated or overlapping completion of one upload answers with the same
// item. An error is returned to the upload handler, which writes it as a
// problem and keeps the upload, so the client may repeat the completing
// request.
func Completion(service *Service, base string) func(http.ResponseWriter, *http.Request, upload.File, io.Reader) error {
	return func(w http.ResponseWriter, r *http.Request, f upload.File, content io.Reader) error {
		item, err := service.CreateFile(r.Context(), CreateFile{
			UploadID:  f.ID,
			Name:      f.Name,
			MediaType: f.MediaType,
			SHA256:    f.Digest,
			Size:      f.Length,
		}, content)
		if err != nil {
			return err
		}
		w.Header().Set("Location", base+"/"+item.ID)
		return web.WriteJSON(w, http.StatusCreated, item)
	}
}

func (h *handler) create(w http.ResponseWriter, r *http.Request) error {
	name, err := recordName(r)
	if err != nil {
		return err
	}
	if err := h.records.WidenUpload(w, r); err != nil {
		return err
	}
	content, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.limit))
	switch {
	case err == nil:
	case errors.Is(err, os.ErrDeadlineExceeded):
		return web.Problem{Status: http.StatusRequestTimeout, Detail: "the record arrived slower than the transfer rate allows"}
	default:
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return &web.BodyError{TooLarge: true, Reason: fmt.Sprintf("exceeds the %d-byte limit", h.limit)}
		}
		return &web.BodyError{Reason: "the record broke off before its end"}
	}
	item, err := h.service.CreateRecord(r.Context(), CreateRecord{Name: name}, content)
	if err != nil {
		return err
	}
	w.Header().Set("Location", r.URL.Path+"/"+item.ID)
	return web.WriteJSON(w, http.StatusCreated, item)
}

func recordName(r *http.Request) (string, error) {
	cd := r.Header.Get("Content-Disposition")
	if cd == "" {
		return "", fmt.Errorf("%w: a record requires a Content-Disposition file name", ErrValidation)
	}
	_, params, err := mime.ParseMediaType(cd)
	if err != nil {
		return "", fmt.Errorf("%w: Content-Disposition %q: %v", ErrValidation, cd, err)
	}
	return params["filename"], nil
}

func (h *handler) find(w http.ResponseWriter, r *http.Request) error {
	id, err := web.PathUUID(r, "id")
	if err != nil {
		return err
	}
	item, err := h.service.Find(r.Context(), id)
	if err != nil {
		return err
	}
	return web.WriteJSON(w, http.StatusOK, item)
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) error {
	q, err := web.ParseQuery(r.URL.Query(), h.limits)
	if err != nil {
		return err
	}
	items, paging, err := h.service.List(r.Context(), q)
	if err != nil {
		return err
	}
	return web.WriteJSON(w, http.StatusOK, web.NewPage(items, q, paging))
}

func status(err error) (web.Problem, bool) {
	switch {
	case errors.Is(err, ErrValidation), errors.Is(err, query.ErrDirectives):
		return web.Problem{Status: http.StatusBadRequest}, true
	case errors.Is(err, sql.ErrNoRows):
		return web.Problem{Status: http.StatusNotFound}, true
	case errors.Is(err, database.ErrNotReady), errors.Is(err, sqlate.ErrConnectionFailed):
		return web.Problem{Status: http.StatusServiceUnavailable}, true
	}
	return web.Problem{}, false
}
