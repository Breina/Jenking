package usecase

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// ArtifactFile is the result of materializing a build artifact to a cache file.
// Text carries the downloaded bytes so callers can serve a small inline window
// without re-reading the file; as with LogFile it must not be emitted wholesale.
type ArtifactFile struct {
	Name        string
	Path        string
	Size        int64
	ContentType string
	Text        string
}

// ArtifactToFile downloads one archived artifact and writes it to a cache file.
// Artifact URLs are behind Jenkins' authentication, so an agent fetching the URL
// from list_artifacts itself gets a 403; this is the authenticated path.
//
// name matches an artifact's display path, its archive-relative path, or its
// bare file name (case-insensitively as a last resort).
func (d Deps) ArtifactToFile(ctx context.Context, jobPath string, buildNum int, name string) (ArtifactFile, error) {
	if d.Store == nil || d.Store.Disk == nil {
		return ArtifactFile{}, fmt.Errorf("artifact file handoff requires the disk cache")
	}
	arts, err := d.ListArtifacts(ctx, jobPath, buildNum)
	if err != nil {
		return ArtifactFile{}, err
	}
	art, err := matchArtifact(arts, name)
	if err != nil {
		return ArtifactFile{}, err
	}
	text, contentType, err := d.Client.GetArtifactContent(ctx, art.URL)
	if err != nil {
		return ArtifactFile{}, err
	}
	p, size, err := d.Store.Disk.SaveArtifact(jobPath, buildNum, art.DisplayPath, text)
	if err != nil {
		return ArtifactFile{}, err
	}
	return ArtifactFile{Name: art.DisplayPath, Path: p, Size: size, ContentType: contentType, Text: text}, nil
}

// matchArtifact picks the artifact addressed by name, preferring an exact
// display-path hit over a base-name hit so a nested artifact is never shadowed
// by a same-named one elsewhere in the archive.
func matchArtifact(arts []jmodel.Artifact, name string) (jmodel.Artifact, error) {
	if name == "" {
		return jmodel.Artifact{}, fmt.Errorf("artifact name is required")
	}
	for _, a := range arts {
		if a.DisplayPath == name {
			return a, nil
		}
	}
	names := make([]string, 0, len(arts))
	for _, a := range arts {
		names = append(names, a.DisplayPath)
		if strings.EqualFold(a.DisplayPath, name) || strings.EqualFold(path.Base(a.DisplayPath), name) {
			return a, nil
		}
	}
	return jmodel.Artifact{}, fmt.Errorf("artifact %q not found; archived artifacts: %s", name, strings.Join(names, ", "))
}
