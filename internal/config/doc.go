// Package config declares the service's layered configuration.
//
//   - [Config] is the root: the library capability blocks (log, server,
//     database, storage, messaging, nats), the service-owned read, upload,
//     and sweep policies, and the shutdown timeout. The database block's password and
//     the storage block's key come from the secrets file (secrets.json,
//     gitignored, shaped as secrets.example.json) or
//     MIKOSHI_DATABASE_PASSWORD and MIKOSHI_STORAGE_KEY, never from a
//     committed configuration file. The storage block is go-storage's: its
//     container is the bucket, its account the access key ID, its key the
//     secret access key, and its region option the region requests are
//     signed for. The messaging block is go-messaging's runtime's, its
//     source the one every event Mikoshi emits carries; the nats block is
//     its NATS provider's, naming the server, the stream Mikoshi owns, and
//     the prefix of Mikoshi's event types. The timeout comes from go-core's lifecycle.Config,
//     embedded by value and untagged, so shutdown_timeout is a top-level
//     key, and Config.Config is the block the composition root hands the
//     lifecycle Coordinator. Its Merge and Finalize delegate to each
//     block's own.
//   - [ReadsConfig] is the service-owned read policy, with its own Merge and
//     Finalize; its Limits method hands the policy to a handler as
//     web.Limits. [ReadsEnv] records the environment-variable names its
//     Finalize read.
//   - [UploadsConfig] is the service-owned upload policy, with its own
//     Merge and Finalize: the lifetime of a resumable upload and the
//     max-append-size the upload handler announces and enforces, which
//     bounds a posted record's body too. [UploadsEnv] records the
//     environment-variable names its Finalize read.
//   - [SweepConfig] is the service-owned sweep policy, with its own Merge
//     and Finalize: a [SweepPolicy], the interval and budget, for each
//     reclaimer the sweeper runs, the pending blobs' and the orphaned
//     blobs' each a [StalePolicy] that adds the age past which a blob row
//     is stale, and the outbox's and the inbox's retentions each a
//     [RetentionPolicy] that adds the age past which a row is removed. [SweepEnv] records the environment-variable names its
//     Finalize read.
//   - [Load] reads the layered files, decoding each strictly, and finalizes
//     the result under the service's env prefix.
//
// The unexported envPrefix const is the single place a seeded service
// renames its environment namespace.
package config
