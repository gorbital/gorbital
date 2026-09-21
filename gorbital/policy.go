package gorbital

import "errors"

// ErrNotImplemented is what a decision the app hasn't made yet returns. It
// exists so that "nobody has written this rule" is a refusal in its own
// right, distinguishable from "the rule ran and said no": a module that
// returns it is unfinished, not restrictive.
//
// orb gen module --scope custom writes a policy.go whose CanRead, CanWrite
// and Filter return it (ADR-0091). The generated module maps it to 501
// not_implemented, its shipped test fails while any of the three still
// returns it, and orb doctor names the module until they don't. An
// unwritten access rule therefore refuses every request and fails the
// build, rather than serving every row to everyone:
//
//	// CanRead reports whether the actor in ctx may read this order.
//	func (p Policy) CanRead(ctx context.Context, order domain.Order) error {
//		return gorbital.ErrNotImplemented // decide: who may read one order?
//	}
//
// Application code is free to return it from anything else it has not
// finished. The framework itself never returns it.
var ErrNotImplemented = errors.New("gorbital: not implemented")
