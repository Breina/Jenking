package jmodel

import "path"

// FindArtifact locates an artifact by its display path, falling back to a
// basename match so callers can pass just the file name of a nested artifact.
func FindArtifact(arts []Artifact, name string) (Artifact, bool) {
	for _, a := range arts {
		if a.DisplayPath == name {
			return a, true
		}
	}
	for _, a := range arts {
		if path.Base(a.DisplayPath) == name {
			return a, true
		}
	}
	return Artifact{}, false
}
