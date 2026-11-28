// Package app is the composition root. It describes the service as one
// go-core dependency graph, and each architecture layer has one file, which
// defines the layer's nodes in its define function and owns its mount, so
// the layer files are the architecture's layer list:
//
//   - infrastructure.go: defineInfrastructure, the lifecycle configuration
//     and the services the application is composed on (the logger, the
//     database pool, the object store, the broker, and the sql node over
//     the pool);
//   - admin.go: defineAdmin, the quiesce gate and the administrative
//     services, and their /admin mount;
//   - domain.go: defineDomain, the domain services, and their /api mount;
//   - reactors.go: defineReactors, the process-lifetime entry points an
//     occurrence drives, each a node whose value is a go-core
//     reactor.Reactor;
//   - client.go: defineClient, the web client's embedded build and the
//     handler that serves its shell and assets, and their mount, the
//     router's not-found handler, so every path no mount or probe claims
//     reaches the client;
//   - server.go: defineServer, the request edge: the readiness the probes
//     report, the router, and the server;
//   - routes.go: the list of mounts;
//   - middleware.go: the router-level middleware stack, outermost first.
//
// [Nodes] holds one handle per node, the single description of what the
// service is composed of: each define function fills its own part of one
// Nodes value, declaring each node's order and group membership on its
// Define line, and its constructors, methods on *Nodes, read the lower
// layers' nodes from it with Use; [Nodes] states how each field names its
// node. [App] is the described process. [New] is the cold start: it
// describes the graph, layer by layer, lowest first, constructs nothing,
// and cannot fail. [App.Graph] and [App.Nodes] publish the graph and its
// handles, so a caller can Observe or Replace a node before Run. [App.Run]
// is the hot start plus shutdown: it runs a go-web-sdk web.Service, which
// builds [App.Roots], the server and the reactors group, plus the
// lifecycle config node, hands the System to go-core's lifecycle
// Coordinator, and returns the process exit code. A constructor's error
// fails the Build, and Run writes "service failed: " and the error, which
// names the failing node, to the stderr it is given; a wiring mistake
// panics during Build.
//
// What a node takes part in is inferred from its value's methods: a
// lifecycle.Starter or Stopper is started or stopped, a Subsystem is both, a
// ReadyStarter (a Starter that also implements Ready) joins the readiness
// probe, and a Monitored has its runtime error watched. A value with Ready
// but no Start joins no readiness check. Nodes start in layer order and
// drain in reverse. The server orders itself after the reactors group, so
// it is alone in the top layer: it starts after every other node and drains
// first, so in-flight requests complete before what they run on closes. The
// service's logger reaches the server, the middleware, and every mount's
// error writer. The probes register on the router's native mux, outside
// every module's middleware, query live on every request through the
// readiness node's value, and report the Coordinator's status under the
// "lifecycle" name, ahead of the nodes' own checks in layer, then
// definition, order.
//
// Routes and reactors are the two ways the running process is entered: a
// caller drives a route, and an occurrence the process receives or
// discovers drives a reactor. A route reads the domain nodes, and a
// reactor may; neither is a domain service itself. Extending the service
// means defining a node in its layer's define function, and joining a
// reactor to the Nodes.Reactors group with graph.In on its Define line;
// the signatures, cmd/server, and [App.Run] stay untouched.
//
// # The layers
//
// The infrastructure layer holds the lifecycle config the Coordinator is
// configured from and the services the application is composed on, read
// from the config node: the logger, written to the writer node; the
// database, the PostgreSQL pool go-database's postgres provider constructs
// from the configuration's database block; and the sql node, the pool's
// session wrapped in sqlate's PostgreSQL dialect and grouped with the
// pattern catalog as a data.Database; and the storage, go-storage's Store
// over the client its s3 provider constructs from the configuration's
// storage block; and the broker, go-messaging's NATS JetStream broker its
// nats provider constructs from the configuration's nats block, the owner
// of Mikoshi's stream. Each service is its own node, with any order it needs
// declared on its Define line. The composition root is the only package
// that imports a provider: the admin service receives the pool as a
// go-database database.DB, the domains receive the sql node's
// data.Database, and a package that keeps objects receives the storage
// node's storage.Store, go-storage's standard tier. The pool's Start pings
// the database, so an unreachable database fails startup with an error
// naming the database node, and its Ready joins the readiness probe under
// that name. The store's Start ensures the configured bucket exists and
// probes it, so an unreachable gateway fails startup with an error naming
// the storage node; its Ready probes the bucket live and joins the
// readiness probe under the storage's name, so an outage after startup
// makes the service not ready without ending it. The broker's Start
// connects to the NATS server and creates the stream or updates it to the
// nats block, so an unreachable server fails startup with an error naming
// the broker node; its Ready joins the readiness probe under the broker's
// name. A service
// whose value implements lifecycle.Subsystem takes part in startup and
// shutdown through its own methods, and its constructor opens nothing:
// connectivity belongs to its Start, so a failed Build leaks no
// connections.
//
// The admin layer is the administrative counterpart of the domain layer:
// one node per admin domain, each administering one infrastructure service
// over the library mechanisms it triggers and reading that service's node
// with Use. One that verifies and corrects the state of the infrastructure
// it administers is a lifecycle participant, and the domains that depend
// on that state Use it. Each admin domain's route group mounts into the
// /admin group, with its service and the logger for its error writer read
// with Use. Mikoshi's one admin domain is the schema: go-database's admin
// service over the pool, a sqlate session wrapped in the PostgreSQL
// dialect, and a migrator over go-messaging's postgres migration set,
// beneath Mikoshi's own, and data.Migrations, that declares pgvector by
// name only, with no seeder and no seed state, and the sql node's
// statements registry as its inventory. The node's value is a [Schema],
// which Uses every domain node so each has registered its statements
// before it is built. Its Start installs the version of pgvector the image
// offers, failing before any migration when the image offers none, then
// applies the set, then prepares every registered statement against the
// migrated schema (data.Database.Verify), go-messaging's outbox and inbox
// statements among them, recorded on the sql node beside the migration
// set, so a statement that does not fit
// fails startup with an error naming the schema node and the statement;
// its Ready joins the readiness probe under the schema's name, false while
// a migration or the declaration is pending or mismatched. Its route group, the admin/database
// package's /database, mounts into the /admin group over the schema node
// and the gate node, so the server reaches the schema through the router.
// The gate is a fresh go-core reactor.Gate, the process's quiesce gate:
// the schema-changing verbs hold it exclusively, and a reactor that works
// over the schema holds it shared for each turn. In production the admin
// mount belongs on its own listener, authenticated and unreachable from
// the public API's network path.
//
// The domain layer has one node per domain layer. Domain services are
// defined in the module's base-layer domain packages, one package per
// layer under domain/, and each node's constructor builds its service from
// the infrastructure nodes it Uses, never from Nodes itself. Domain
// services own no resource and never run, so none is a lifecycle
// participant. A package that does own a resource and run belongs in the
// infrastructure layer, if it is built once at startup, or the reactors
// layer, if it reacts to an external occurrence for the life of the
// process. Each domain layer's route group mounts into the /api group, its
// handler handed, at the construction site, its service, its policy from
// the config node (cfg.Reads.Limits() for a collection read), and the
// logger for its error writer (web.NewErrorWriter(logger, ...)). Mikoshi's
// domain layers are contexts, the context tree, built over the sql node,
// the items layer's FilterFields, the field contract its query filters are
// checked against, and the runtime's Recorder, which its commands emit
// their events through, and mounted at /api/contexts; uploads, the resumable
// uploads in progress, built over the sql node; and items, built over the
// sql node, the storage node, the contexts node, and the runtime's
// Recorder, which its commands emit their events through, and mounted at
// /api/items, each read with the read limits allowing a cursor. The items
// layer's reference group mounts at /api/contexts beside the contexts
// layer's group, its routes under /api/contexts/{id}/items, the covered
// items' read with the same read limits. go-web-sdk's draft-16 upload
// handler mounts at /api/uploads, on the client's origin and so with no
// CORS, over the uploads service, with the lifetime and max-append-size of
// the configuration's uploads block, the max-append-size as the limit of
// the server block's Transfer, and the items layer's Completion as its
// Complete, which answers with the item's Location under /api/items. The
// items group's record create reads its body under that same Transfer.
//
// A reactor runs for the process lifetime, driven by an occurrence (a
// subscription, an interval, a wake on demand) rather than a caller, the
// inbound counterpart to a route. It often dispatches each occurrence to a
// domain service call, but need not. Each reactor is its own node, a
// lifecycle participant whose constructor Uses the infrastructure nodes for
// the transport connection it owns and the domain nodes it dispatches to.
// The node's value is a go-core reactor.Reactor, which reactor.New returns
// over a Source, a handler, and a reactor.Options. The Source is
// reactor.Every for a plain interval, or reactor.Wake for an interval that
// a nudge can also trigger. reactor.Options.CancelAfter sets how long a
// drain lets the handling in flight run before it cancels it, and
// reactor.CancelWithin returns half the shutdown timeout for that field;
// under the zero Options, the drain cancels the handling only when its
// context ends. The Coordinator infers from the value's methods that the
// reactor starts with its layer, drains before the nodes it uses, joins
// the readiness probe under its node's name, and ends the run on its
// failure. Mikoshi has two reactors. The relay is the reactor the runtime
// node builds: go-messaging's Runtime over the broker and its postgres
// engine's outbox and inbox statements, whose Recorder stamps the layer
// extension, mikoshi, on every event a domain emits. The relay orders
// itself after the schema, publishes each committed outbox row to the
// broker, and holds the gate shared for each row, so it pauses while an
// admin verb holds the gate. The sweeper is go-database's
// sweep.Sweeper, a reactor of the same shape over a reactor.Wake: it
// orders itself after the schema, so it starts over the migrated schema,
// holds the gate shared for each pass, and runs the domains' reclaimers,
// each named for the advisory lock it claims and scheduled by its policy
// from the configuration's sweep block: expired_uploads, the uploads
// layer's Reclaimer, which deletes expired uploads and their chunks;
// pending_blobs, the items layer's PendingReclaimer, which removes a blob
// row left pending past the block's stale age, and its object; and
// orphan_blobs, the items layer's OrphanReclaimer, which removes an
// available blob no item has named past the block's stale age, its
// object, and the content's source occurrences, sources, chunks, and
// embeddings that nothing else names; outbox_messages, go-messaging's
// outbox retention, which removes the published outbox rows past the
// block's age; and inbox_messages, its inbox retention, which removes the
// handled inbox rows past the block's age.
//
// The request edge's readiness is a fresh node, so each Build's starts
// unbound, ready for the Coordinator that Build's System is handed to. The
// probes register with the zero web.Problem, which keeps the SDK's
// readiness defaults: type about:blank, status 503 with its status text as
// the title, the generic detail, and the checks extension member. A service
// passes its own not-ready problem type once it has one.
//
// The middleware stack reads infrastructure nodes only: request logging,
// and cross-cutting concerns like it, need infrastructure primitives, not
// domain services. A middleware that has to reach a domain service is
// domain logic, and belongs in a route or a reactor instead. RequestID
// runs ahead of RequestLogger, so the log record carries the request id.
// Recoverer turns a handler's panic into a logged 500, which RequestLogger
// records with its status; it works on either side of RequestLogger, and
// the template places it inside. A BodyLimit, when a service adds one,
// goes ahead of RequestLogger and Recoverer; the chain order in
// go-web-sdk's middleware package documentation states why.
package app
