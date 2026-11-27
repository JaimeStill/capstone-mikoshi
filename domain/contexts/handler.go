package contexts

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"

	"github.com/standards-lab/go-database"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"
)

const maxCommandBody = 1 << 16

type handler struct {
	service *Service
}

// Routes builds the layer's route group, /contexts: the create and the
// read by id. logger records the cause of every 5xx the group's error
// writer sends. Every refusal is a problem response: invalid input a 400,
// an unknown context or parent a 404, and a sibling's name or a query
// parent a 409; an unreachable database is a 503.
func Routes(service *Service, logger *slog.Logger) *web.Group {
	h := &handler{service: service}
	g := web.NewGroup("/contexts")
	g.SetErrorWriter(web.NewErrorWriter(logger, status))
	g.HandleErr("POST", "", h.create)
	g.HandleErr("GET", "/{id}", h.find)
	return g
}

func (h *handler) create(w http.ResponseWriter, r *http.Request) error {
	body, err := web.DecodeJSON[CreateContext](w, r, maxCommandBody)
	if err != nil {
		return err
	}
	c, err := h.service.Create(r.Context(), body)
	if err != nil {
		return err
	}
	w.Header().Set("Location", r.URL.Path+"/"+c.ID)
	return web.WriteJSON(w, http.StatusCreated, c)
}

func (h *handler) find(w http.ResponseWriter, r *http.Request) error {
	id, err := web.PathUUID(r, "id")
	if err != nil {
		return err
	}
	c, err := h.service.Find(r.Context(), id)
	if err != nil {
		return err
	}
	return web.WriteJSON(w, http.StatusOK, c)
}

func status(err error) (web.Problem, bool) {
	switch {
	case errors.Is(err, ErrValidation):
		return web.Problem{Status: http.StatusBadRequest}, true
	case errors.Is(err, ErrParentNotFound):
		return web.Problem{Status: http.StatusNotFound, Detail: ErrParentNotFound.Error()}, true
	case errors.Is(err, sql.ErrNoRows):
		return web.Problem{Status: http.StatusNotFound}, true
	case errors.Is(err, ErrNameTaken):
		return web.Problem{Status: http.StatusConflict, Detail: ErrNameTaken.Error()}, true
	case errors.Is(err, ErrQueryParent):
		return web.Problem{Status: http.StatusConflict, Detail: ErrQueryParent.Error()}, true
	case errors.Is(err, database.ErrNotReady), errors.Is(err, sqlate.ErrConnectionFailed):
		return web.Problem{Status: http.StatusServiceUnavailable}, true
	}
	return web.Problem{}, false
}
