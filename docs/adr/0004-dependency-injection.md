# ADR-004: Dependency injection

**Status:** Proposed

**Context:** Apps compose a dozen components with lifecycles.

**Options:** Fx (runtime reflection), Wire (compile-time codegen, archived 2025), custom container, manual constructor injection.

**Decision:** Manual constructor injection in one owned `app.go`, plus `app.Run` with `Starter/Stopper` interfaces.

**Why:** Zero magic, compile-time errors, readable by every Go developer and AI tool. The generator writes the boring part, which was the only real argument for a container.

**Tradeoffs:** `app.go` grows in large apps (acceptable; can be split by hand).

**Consequences:** Modules must accept dependencies as parameters, never look them up globally.
