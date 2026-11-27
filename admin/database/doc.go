// Package database is the database admin domain: the HTTP half of the
// database admin service, a route group under the admin mount over
// go-database's admin.Service, whose operations are the library functions
// startup runs. The admin mount is the /admin route group, the
// administrative counterpart of /api; an admin domain is a package under
// admin/ named for the infrastructure service it administers.
//
// The mount is not for public deployment. It serves on the API listener,
// unauthenticated, until a management listener gives it an authenticated
// listener of its own; a deployment keeps the API listener off every
// public network path until then.
//
// Mikoshi declares no seed states, so the states read lists none, and the
// seed verb and a confirmed state reset answer 403. A reset emits no
// event.
//
// The package's API:
//
//   - [Routes]: the route group, its schema-changing verbs holding a
//     [SchemaGate] exclusively.
//   - [Steps], [Force], [State], and [Reset]: the verbs' request bodies.
//   - [ErrUnconfirmed]: a reset whose body does not confirm it, a 400.
package database
