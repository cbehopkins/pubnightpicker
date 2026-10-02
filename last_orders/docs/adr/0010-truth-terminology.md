# ADR: Retire "Fact" in Favour of "Truth"

## 1. Status

Accepted

---

## 2. Context

ADR-0005 §11 introduced the term **Fact** for something observed by a Listener which may cause application work. ADR-0009 later introduced **Truth** to describe the same durable, typed statement, independent of its source, with the added requirements that a Truth is a concrete Go type carrying its own evidence and identity.

These were never two different concepts. They were the same idea named twice, in two different ADRs, at two different points in the system's development. In practice this produced:

* a `components/facts` package (a generic `Fact{Name, Payload}` envelope, a string-keyed `Registry`, and a single shared `cellar.Fanout[Fact]`);
* a `truths` package (typed structs such as `EventVenueObserved`, each with an `Identity()` method);
* several more Truth names declared as bare string constants local to whichever plugin emitted them (`polls.FactNewPoll`, `logsvc.FactLogMessage`, `recurrenceplugin.FactStaleEvent`, ...), disconnected from both of the above.

This split the catalogue of "things the system can observe" across four uncoordinated locations, with no shared namespace to catch accidental name collisions, and left plugin code importing both `facts` and `truths` for what is conceptually one idea.

---

## 3. Decision

The term **Fact** is retired. **Truth**, as defined in ADR-0009, is the only term used going forward, in documentation and in code.

* ADR-0005 §11 is superseded by this decision; it remains in place as a historical record of the original terminology but no longer reflects current usage.
* Every Truth name and its associated Go payload type is declared in exactly one place: the `truths` package. Nothing else is permitted to declare a Truth name locally.
* Dispatch of a Truth to its registered handlers is implemented using Cellar's native generic `cellar.Fanout[T]`, constructed once per concrete Truth type `T`, rather than a single hand-rolled `Fanout` over a generic string-keyed envelope. This gives Cellar responsibility for decoding each Truth's own type directly, rather than decoding a generic envelope and re-decoding a raw payload a second time per handler.
* The Idempotency Layer (ADR/CDD 0001) remains generic across Truth types, since it must be reusable; it carries a Truth's dispatch target and JSON payload without needing to know the concrete Go type.

---

## 4. Consequences

**Positive**

* One namespace for the catalogue of Truths, making accidental name collisions visible instead of silent.
* Removes the redundant `facts`/`truths` split and the resulting dual imports in plugin code.
* Truth dispatch uses Cellar's own typed Fanout mechanism instead of a parallel, less type-safe implementation.

**Negative**

* Existing code and documentation referencing "Fact" requires a mechanical rename.

---

## 5. Relationship to Other ADRs

* Supersedes ADR-0005 §11 ("Facts").
* Implements the type-per-Truth model already required by ADR-0009.
