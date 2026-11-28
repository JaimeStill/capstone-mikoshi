package contexts

import (
	"context"

	"github.com/standards-lab/go-core/event"
	"github.com/standards-lab/sqlate"

	"github.com/JaimeStill/capstone-mikoshi/correlation"
)

// The events the layer's commands emit, one per command that changes the
// tree, each committed in the transaction that makes the command's change
// true, so it is published exactly when the change stands, and never for a
// command that is refused, fails, or changes nothing. Each event's subject
// is the context's id. A delete emits one event, on the context it names;
// its descendants and their references emit none. Reads and the subtree
// resolution emit nothing. Their types and their data are public API: a
// consumer in another service binds both.
var (
	// ContextCreated is the event a create emits, its data the context as
	// the insert stored it.
	ContextCreated = event.Define[ContextData]("mikoshi.context.created")
	// ContextRenamed is the event a rename emits, its data the context as
	// the rename left it. A rename to the name the context already has
	// emits nothing.
	ContextRenamed = event.Define[ContextData]("mikoshi.context.renamed")
	// ContextMoved is the event a move emits, with or without a rename, its
	// data the context as the move left it, under its new parent. A move
	// to the parent and the name the context already has emits nothing.
	ContextMoved = event.Define[ContextData]("mikoshi.context.moved")
	// ContextDeleted is the event a delete emits, its data the deleted
	// context's id.
	ContextDeleted = event.Define[ContextDeletedData]("mikoshi.context.deleted")
)

// Recorder is the recorder the layer emits its events through, in the
// command's own transaction: go-messaging's Runtime.Recorder, which the
// composition root injects.
type Recorder = event.Recorder[*sqlate.Tx]

// ContextData is the data of ContextCreated, ContextRenamed, and
// ContextMoved: the context's stored state after the command, under the
// read's field names, without its creation time. Filter is null on an
// explicit context.
type ContextData struct {
	ID       string  `json:"id"`
	ParentID *string `json:"parent_id"`
	Name     string  `json:"name"`
	Kind     Kind    `json:"kind"`
	Filter   Filter  `json:"filter"`
}

// ContextDeletedData is the data of ContextDeleted: the deleted context's
// id.
type ContextDeletedData struct {
	ID string `json:"id"`
}

func raiseStored(ctx context.Context, q *event.Queue, kind event.EventKind[ContextData], c Context) {
	kind.Raise(q, c.ID, ContextData{ID: c.ID, ParentID: c.ParentID, Name: c.Name, Kind: c.Kind, Filter: c.Filter}, correlation.RaiseOptions(ctx))
}
