package items

import (
	"context"

	"github.com/standards-lab/go-core/event"
	"github.com/standards-lab/sqlate"

	"github.com/JaimeStill/capstone-mikoshi/correlation"
)

// The events the layer's commands emit, one per command that changes an
// item or a reference, each committed in the transaction that makes the
// command's change true, so it is published exactly when the change
// stands, and never for a command that is refused, fails, or changes
// nothing. Reads, the covered read, and the reclaimers emit nothing.
// Their types and their data are public API: a consumer in another
// service binds both.
var (
	// ItemCreated is the event a file or a record create emits, about the
	// item's id, its data the item as the insert stored it. A repeated or
	// overlapping completion that returns an item another created emits
	// nothing.
	ItemCreated = event.Define[ItemData]("mikoshi.item.created")
	// ItemDeleted is the event a delete emits, about the item's id, its
	// data the deleted item's id. The references the delete removes with
	// the item emit nothing.
	ItemDeleted = event.Define[ItemDeletedData]("mikoshi.item.deleted")
	// ContextItemAdded is the event a placing emits, about the context's
	// id, when it writes a reference. A reference already in place emits
	// nothing.
	ContextItemAdded = event.Define[ReferenceData]("mikoshi.context.item.added")
	// ContextItemRemoved is the event a withdrawing emits, about the
	// context's id, when it removes a reference. A reference not in place
	// emits nothing.
	ContextItemRemoved = event.Define[ReferenceData]("mikoshi.context.item.removed")
)

// Recorder is the recorder the layer emits its events through, in the
// command's own transaction: go-messaging's Runtime.Recorder, which the
// composition root injects.
type Recorder = event.Recorder[*sqlate.Tx]

// ItemData is the data of ItemCreated: the item as the read presents it,
// under the read's field names, without its creation time. Type is file
// or record.
type ItemData struct {
	ID        string   `json:"id"`
	Type      Type     `json:"type"`
	Name      string   `json:"name"`
	MediaType string   `json:"media_type"`
	SHA256    string   `json:"sha256"`
	Size      int64    `json:"size"`
	Route     string   `json:"route"`
	Kind      *string  `json:"kind"`
	Duration  *float64 `json:"duration"`
}

// ItemDeletedData is the data of ItemDeleted: the deleted item's id.
type ItemDeletedData struct {
	ID string `json:"id"`
}

// ReferenceData is the data of ContextItemAdded and ContextItemRemoved:
// the reference's context and item.
type ReferenceData struct {
	ContextID string `json:"context_id"`
	ItemID    string `json:"item_id"`
}

func raiseCreated(ctx context.Context, q *event.Queue, i Item) {
	ItemCreated.Raise(q, i.ID, ItemData{
		ID: i.ID, Type: i.Type, Name: i.Name, MediaType: i.MediaType, SHA256: i.SHA256,
		Size: i.Size, Route: i.Route, Kind: i.Kind, Duration: i.Duration,
	}, correlation.RaiseOptions(ctx))
}

func raiseReference(ctx context.Context, q *event.Queue, kind event.EventKind[ReferenceData], contextID, itemID string) {
	kind.Raise(q, contextID, ReferenceData{ContextID: contextID, ItemID: itemID}, correlation.RaiseOptions(ctx))
}
