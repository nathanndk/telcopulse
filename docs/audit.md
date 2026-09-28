# Operations audit register

`/audit` presents an environment-scoped, read-only timeline of three existing append-only sources: incident lifecycle audit rows, simulation start/stop audit rows, and deployment status events. Each row identifies the actor, action, recorded time, source and resource. Resource links lead to the owning incident, simulation or deployment investigation. The gateway endpoint is `GET /api/v1/audit`; it accepts `environment`, `source`, `actor`, `action`, `resource`, `limit` and a filter-bound keyset `cursor`.

The register projects incident field changes from the immutable before/after snapshots. It includes old and new values only for state, severity, owner and owning team. Other incident fields appear as change markers. The response omits full snapshots and free-form audit notes to avoid leaking customer or investigation detail into a broad register. The source incident remains the place to review the complete authorized history.

The feed uses event recorded time and a stable source ID for newest-first paging. It is a projection over independently written sources, not a globally serializable audit ledger: newly committed events may appear ahead of an existing cursor. Authentication security events, customer transaction events, and operator account changes are not yet included. Current retention and export policy still need production design.
