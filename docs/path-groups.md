# Path groups

Groups organize Paths and provide batch-selection shortcuts. Permission belongs
only to the user/Path pair. Editing, disabling or archiving a group never changes
previously selected user permissions.

## Using groups

Create and edit groups under **Paths → Path groups**. In **Users → Manage
access**, each group is a collapsible section with a three-state checkbox and a
selected/available count. Use the arrow to expand; the checkbox changes selection.

- Empty: none of the enabled member Paths are selected.
- Checked: all enabled members are selected.
- Mixed (`−`): some are selected. Clicking a mixed group selects the rest.
- Clicking a checked group clears all enabled members, including shared Paths.
- A Path appearing in several groups has one shared selection state. Changing it
  updates every occurrence and each group's count/state immediately.
- Disabled Path checkboxes preserve their selection and cannot change; they are
  excluded from the group's available count and batch operation.
- Ungrouped Paths appear under **Other**. Groups do not nest.

Checkboxes edit a local draft without making requests. **Save** commits all
changed Paths in one atomic batch and closes the dialog; **Cancel** discards the
draft. Save is disabled without changes. Escape and backdrop dismissal are
blocked while there are unsaved changes or a save is in progress. A failed save
retains the draft and displays an error for retry. There is no second confirmation.
Only changed Paths are submitted, preserving unrelated permissions.

A group is a snapshot shortcut: adding a new member tomorrow does not give it to
users who selected the group yesterday. Overlapping groups do not create separate
authorization sources or reference counts. Group inventory **Users** counts users
selecting all its currently enabled member Paths.

Publish changed node configurations through the existing publish workflow.
Clients refresh their configuration to obtain the changed Path list. Removing one
Path does not invalidate a ProxyCredential still required by another Path or its
dialer chain, and does not promise immediate termination of existing connections.

## Storage and migration

Migration 034 introduced group inventory and authorization-source tables.
Migration 035 snapshots active legacy group sources into individual grants,
retains existing individual grants and PathAccess projection/credential rows,
then clears `user_path_group_grants`. Selections, identities, credentials and
billing remain intact. The legacy table remains for schema compatibility. Down
migration preserves individual selection; it cannot reconstruct group origins.

`direct_path_grants` stores individual intent; `path_accesses` is the projection
consumed by publication. Node/Proxy retirement and user deletion retain existing
atomic cleanup and historical-ID rules.

## APIs

All routes use existing admin authentication and mount:

| Route | Purpose |
| --- | --- |
| `GET /api/admin/path-groups` | Members and fully selected user count |
| `POST /api/admin/path-groups/preview` | Internal preflight for `save` / `delete` |
| `POST /api/admin/path-groups/changes` | Inventory changes with preflight token |
| `GET /api/admin/path-groups/{id}/history` | Latest 100 inventory operations |
| `PATCH /api/admin/users/{id}/paths` | Atomic `{changes:[{path_id,enabled}]}` |
| `GET /api/admin/users/{id}/paths` | Current effective selection |

Selection batches contain 1–200 unique selectable Paths. New selections require
enabled Paths. Validation, technical credentials, bindings and projection are
handled in one transaction. Legacy `grant` / `revoke` group operations return 410;
clients must use Path selection. Existing individual POST/DELETE routes remain.
Inventory edits retain internal preview tokens for concurrent-change checks,
without an additional UI confirmation.

Design history: [original sustained-authorization plan](path-groups-plan.md).
