// Package integration is the service's integration tier: the harness that
// runs the composed service as the binary and drives it through its
// production seams, and, under the integration build tag, the suite that
// asserts the service's behavior through its API. The harness is built on
// the SDKs' test toolkits, go-core's processtest and go-web-sdk's webtest;
// nothing in the runtime exists for the tests' sake.
//
//   - [Main] is the suite's TestMain: it builds cmd/server once per run.
//   - [Database] makes an empty throwaway database through sqlate's
//     pgtest, on the PostgreSQL server whose admin DSN [DSNEnv] holds, and
//     drops it when the test ends.
//   - [Migrated] makes such a database with Mikoshi's migrations applied,
//     for a test that runs a domain's store without a service process.
//   - [Bucket] makes a client of one bucket on the SeaweedFS S3 gateway
//     whose endpoint [StorageEnv] holds, the gateway every service process
//     stores its objects in, and [Objects] a started go-storage Store over
//     such a client, for a domain run without a service process.
//   - [Broker] makes a started go-messaging NATS broker on the server
//     whose URL [NATSEnv] holds, the server every service process
//     connects to, bound to [EventStream] as a consumer, never its owner;
//     [Subscribe] holds a durable consumer of the stream through such a
//     broker, and its [Stream] matches the events it receives by subject.
//   - [Options] holds the database one service process connects to and the
//     MIKOSHI_* overrides it adds.
//   - [Start] runs the service on a reserved port, against its own
//     throwaway database unless [Options] shares one, and returns once it
//     is live; [Launch] returns without waiting, and [Service.Ready] waits.
//   - [Service] is one running process: [Service.Addr], [Service.URL], and
//     [Service.Client] reach it, and the embedded processtest.Process
//     stops it and reads its output and exit code.
//
// The harness is untagged, so the unit tier type-checks it on every pull
// request; the suite files carry the integration tag and need the server
// [DSNEnv] names, the gateway [StorageEnv] names, and the NATS server
// [NATSEnv] names, the integration stack the integration task starts.
package integration
