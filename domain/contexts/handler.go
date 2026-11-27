package contexts

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/standards-lab/go-database"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
)

const maxCommandBody = 1 << 16

type handler struct {
	service *Service
	limits  web.Limits
}

// Routes builds the layer's route group, /contexts: the create, the read
// by id, the read of a context's children, paged under limits, the
// rename, a PUT of the context's name, the move, a POST that sets the
// context's parent and, optionally, its name, and the delete, which
// removes the context's subtree and answers 204 when its confirm query
// parameter, absent for zero, is the number of the context's descendants.
// logger records the cause of every 5xx the group's error writer sends.
// Every refusal is a problem response: invalid input or a cursor the read
// did not issue a 400, an unknown context or parent a 404, and a sibling's
// name, a query parent, a rename, move, or delete of the root, a move into
// the context's own subtree, or a delete whose confirm is not the count a
// 409, the last carrying the count as its descendants member; an
// unreachable database is a 503.
func Routes(service *Service, limits web.Limits, logger *slog.Logger) *web.Group {
	h := &handler{service: service, limits: limits}
	g := web.NewGroup("/contexts")
	g.SetErrorWriter(web.NewErrorWriter(logger, status))
	g.HandleErr("POST", "", h.create)
	g.HandleErr("GET", "/{id}", h.find)
	g.HandleErr("GET", "/{id}/children", h.children)
	g.HandleErr("PUT", "/{id}", h.rename)
	g.HandleErr("POST", "/{id}/move", h.move)
	g.HandleErr("DELETE", "/{id}", h.delete)
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

func (h *handler) children(w http.ResponseWriter, r *http.Request) error {
	id, err := web.PathUUID(r, "id")
	if err != nil {
		return err
	}
	q, err := web.ParseQuery(r.URL.Query(), h.limits)
	if err != nil {
		return err
	}
	items, paging, err := h.service.Children(r.Context(), id, q)
	if err != nil {
		return err
	}
	return web.WriteJSON(w, http.StatusOK, web.NewPage(items, q, paging))
}

func (h *handler) rename(w http.ResponseWriter, r *http.Request) error {
	id, err := web.PathUUID(r, "id")
	if err != nil {
		return err
	}
	body, err := web.DecodeJSON[RenameContext](w, r, maxCommandBody)
	if err != nil {
		return err
	}
	c, err := h.service.Rename(r.Context(), id, body)
	if err != nil {
		return err
	}
	return web.WriteJSON(w, http.StatusOK, c)
}

func (h *handler) move(w http.ResponseWriter, r *http.Request) error {
	id, err := web.PathUUID(r, "id")
	if err != nil {
		return err
	}
	body, err := web.DecodeJSON[MoveContext](w, r, maxCommandBody)
	if err != nil {
		return err
	}
	c, err := h.service.Move(r.Context(), id, body)
	if err != nil {
		return err
	}
	return web.WriteJSON(w, http.StatusOK, c)
}

func (h *handler) delete(w http.ResponseWriter, r *http.Request) error {
	id, err := web.PathUUID(r, "id")
	if err != nil {
		return err
	}
	confirm, err := confirmation(r)
	if err != nil {
		return err
	}
	if err := h.service.Delete(r.Context(), id, confirm); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func confirmation(r *http.Request) (int64, error) {
	values, ok := r.URL.Query()["confirm"]
	if !ok {
		return 0, nil
	}
	if len(values) == 1 {
		if n, err := strconv.ParseUint(values[0], 10, 63); err == nil {
			return int64(n), nil
		}
	}
	return 0, fmt.Errorf("%w: confirm must be one non-negative integer, not %q", ErrValidation, values)
}

func status(err error) (web.Problem, bool) {
	if d, ok := errors.AsType[*DescendantsError](err); ok {
		return web.Problem{Status: http.StatusConflict, Detail: ErrUnconfirmed.Error(), Extras: map[string]any{"descendants": d.Count}}, true
	}
	switch {
	case errors.Is(err, ErrValidation), errors.Is(err, query.ErrDirectives):
		return web.Problem{Status: http.StatusBadRequest}, true
	case errors.Is(err, ErrParentNotFound):
		return web.Problem{Status: http.StatusNotFound, Detail: ErrParentNotFound.Error()}, true
	case errors.Is(err, sql.ErrNoRows):
		return web.Problem{Status: http.StatusNotFound}, true
	case errors.Is(err, ErrNameTaken):
		return web.Problem{Status: http.StatusConflict, Detail: ErrNameTaken.Error()}, true
	case errors.Is(err, ErrQueryParent):
		return web.Problem{Status: http.StatusConflict, Detail: ErrQueryParent.Error()}, true
	case errors.Is(err, ErrRootFixed):
		return web.Problem{Status: http.StatusConflict, Detail: ErrRootFixed.Error()}, true
	case errors.Is(err, ErrCycle):
		return web.Problem{Status: http.StatusConflict, Detail: ErrCycle.Error()}, true
	case errors.Is(err, database.ErrNotReady), errors.Is(err, sqlate.ErrConnectionFailed):
		return web.Problem{Status: http.StatusServiceUnavailable}, true
	}
	return web.Problem{}, false
}
