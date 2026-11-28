package correlation

import (
	"context"
	"uuid"

	"github.com/standards-lab/go-core/event"
	"github.com/standards-lab/go-web-sdk"
)

// Extension is the name of the extension attribute that carries an
// event's correlation id.
const Extension = "correlationid"

// ID returns the correlation id of the command ctx belongs to: the request
// id go-web-sdk's RequestID middleware set on ctx, or, when ctx carries
// none or an empty one, a fresh version 7 UUID. Outside a request each
// call mints a new id, so a command calls it once and shares the result
// among its events.
func ID(ctx context.Context) string {
	if id, ok := web.RequestIDFrom(ctx); ok && id != "" {
		return id
	}
	return uuid.NewV7().String()
}

// RaiseOptions returns the options of each raise in the command ctx
// belongs to: [Extension] set to [ID] of ctx, and no other attribute.
func RaiseOptions(ctx context.Context) event.RaiseOptions {
	return event.RaiseOptions{Extensions: map[string]string{Extension: ID(ctx)}}
}
