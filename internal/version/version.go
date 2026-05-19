package version

// App is injected at build time via -ldflags; falls back to "dev" for local builds.
var App = "dev"
