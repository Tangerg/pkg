// Package mime parses, compares, and detects MIME types.
//
// A MIME type is constructed with [New], [Parse], or a fluent
// [Builder]; content can be sniffed with [Detect], [DetectReader],
// or [DetectFile]. Use [TypeByExtension] / [StringTypeByExtension]
// to look up a type from a file extension and [RegisterExtension] /
// [RegisterExtensions] to extend the table.
//
// [MIME] values expose component accessors ([MIME.Type],
// [MIME.SubType], [MIME.Charset], [MIME.Param]), comparison helpers
// ([MIME.Equals], [MIME.EqualsTypeAndSubtype], [MIME.IsCompatibleWith]),
// and matching predicates ([MIME.IsWildcardType], [MIME.IsConcrete],
// [MIME.Includes], [MIME.IsPresentIn]), which treat wildcards and
// parameters as patterns to satisfy.
//
// A built [MIME] is immutable and safe for concurrent use: [MIME.String]
// renders parameters in ascending key order, and parameters are reported
// with their quoting removed.
//
// Category helpers [IsText], [IsImage], [IsAudio], [IsVideo], and
// [IsApplication] test the primary type. [NormalizeXSubtype] folds
// legacy "x-" subtypes (RFC 6648) onto their modern equivalents, which
// [RegisterXSubtype] / [RegisterXSubtypes] can extend.
package mime
