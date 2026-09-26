// Package text provides string-line manipulation helpers and a small
// fluent template renderer.
//
// Line operations: [Lines], [AlignToLeft], [AlignToRight], [AlignCenter],
// [TrimAdjacentBlankLines], [DeleteTopLines], [DeleteBottomLines].
//
// Templating: [Renderer] (chainable, caches the parsed template, never rendered
// output) or the one-shot [Render] / [MustRender] functions.
package text
