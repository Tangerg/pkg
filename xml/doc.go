// Package xml is a streaming XML element scanner geared at LLM
// output processing — extracting structured `<think>` / `<tool_use>`
// / custom tags from a token stream as they arrive, not after the
// stream closes.
//
// [StreamScanner] consumes bytes incrementally and fires
// [ElementListener] callbacks as soon as each registered element
// closes. Buffering is capped so that a runaway tag in the model
// output can't blow up memory: [ElementListener.MaxBufferSize] bounds
// each tracked element and [StreamScannerConfig.MaxTextBufferSize]
// bounds the text passed through outside tracked elements. Reaching a
// cap drops the excess and reports it — fatally in strict mode, through
// [StreamScannerConfig.OnError] in non-strict mode.
//
// Every buffer the scanner holds is capped, so no input can grow one
// past the configured budgets: an element no listener tracks shares the
// cap of the tracked element enclosing it, and the tag being read — the
// one buffer that is not element or text content — is capped by the
// largest budget the caller configured.
//
// The package complements (does not replace) [encoding/xml]: stdlib
// is best when the document is well-formed and you have it in
// memory; this package is best for partial / streaming / LLM-tainted
// input where you want known tags pulled out and the rest passed
// through verbatim.
package xml
