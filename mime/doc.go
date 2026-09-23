// Package mime parses, compares, and detects MIME types.
//
// A MIME type is constructed with [New], [Parse], or a fluent [Builder];
// content can be sniffed with [Detect], [DetectReader], or [DetectFile].
// Use [TypeByExtension] / [StringTypeByExtension] to look up a type from a
// file extension and [RegisterExtension] / [RegisterExtensions] to extend the
// table.
//
// Types and subtypes are lower-cased, parameter names lower-cased, and
// charset values upper-cased; accessors such as [MIME.Param] report parameter
// values with their quoting removed. A [MIME] returned by a constructor is
// immutable and safe for concurrent use, and [MIME.String] renders it with
// parameters in ascending key order.
//
// [MIME] exposes component accessors ([MIME.Type], [MIME.SubType],
// [MIME.Charset], [MIME.Param]), comparison helpers such as [MIME.Equals],
// [MIME.EqualsParams], and [MIME.IsMoreSpecific], and matching predicates such
// as [MIME.Includes], [MIME.IsCompatibleWith], and [MIME.IsPresentIn], which
// treat wildcards and parameters as patterns to satisfy.
//
// Category helpers [IsText], [IsImage], [IsAudio], [IsVideo], and
// [IsApplication] test the primary type. [NormalizeXSubtype] folds legacy "x-"
// subtypes (RFC 6648) onto their modern equivalents, which [RegisterXSubtype] /
// [RegisterXSubtypes] can extend.
package mime
