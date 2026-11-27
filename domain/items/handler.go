package items

import (
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/standards-lab/go-database"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/upload"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
)

type handler struct {
	service *Service
	limits  web.Limits
}

// Routes builds the layer's route group, /items: the read by id and the
// paged read of every item, under limits. logger records the cause of
// every 5xx the group's error writer sends. Every refusal is a problem
// response: a malformed id, a sort or filter, or a cursor the read did not
// issue a 400, an unknown item a 404, and an unreachable database a 503.
func Routes(service *Service, limits web.Limits, logger *slog.Logger) *web.Group {
	h := &handler{service: service, limits: limits}
	g := web.NewGroup("/items")
	g.SetErrorWriter(web.NewErrorWriter(logger, status))
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
