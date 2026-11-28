// Package correlation gives a command the correlation id its events
// carry, so a consumer can group every event one request caused. A domain
// package that emits events calls [RaiseOptions] once per command and
// passes the result to each of the command's raises; the composition root
// stamps the layer on every event through the recorder, so the per-raise
// options carry the correlation id alone and no causation id.
//
// The package exports:
//
//   - [Extension], the extension attribute's name, correlationid
//   - [ID], the command's correlation id: the id of the request ctx
//     carries, or a fresh version 7 UUID when the command runs outside a
//     request
//   - [RaiseOptions], the event.RaiseOptions that set [Extension] to [ID]
package correlation
