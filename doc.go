// Package jollyroger is an embedded feature-flag library: feature flags without another server to
// maintain.
//
// jollyroger runs inside your application, stores flags in the PostgreSQL or SQLite database you
// already have, evaluates them from an in-process cache, and can mount an authenticated dashboard
// on your own router.
//
// The library is under active development and has no public API yet. See the design document in
// docs/superpowers/specs and the roadmap in the README.
package jollyroger
