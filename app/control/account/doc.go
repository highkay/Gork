// Package account owns account records, capability metadata, quota state, and
// repository-facing account mutation commands. Callers should depend on the
// Repository and AccountRefreshService seams; concrete local, Redis, and SQL
// storage details live behind backend adapters.
package account
