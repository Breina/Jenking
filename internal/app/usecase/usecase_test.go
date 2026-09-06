package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/navmsg"
)

// fakeClient implements only the methods the resolution helpers call. Embedding
// the port interface means every other method exists (and panics if called), so
// tests stay small.
type fakeClient struct {
	jmodel.JenkinsClient
	builds        []jmodel.Build
	buildsErr     error
	projectBuilds []jmodel.ProjectBuild
	projectErr    error
	whoami        *jmodel.User
	whoamiErr     error
}

func (f fakeClient) ListBuilds(context.Context, string) ([]jmodel.Build, error) {
	return f.builds, f.buildsErr
}
func (f fakeClient) ListProjectBuilds(context.Context, string) ([]jmodel.ProjectBuild, error) {
	return f.projectBuilds, f.projectErr
}
func (f fakeClient) WhoAmI(context.Context) (*jmodel.User, error) {
	return f.whoami, f.whoamiErr
}

type notFoundErr struct{}

func (notFoundErr) Error() string       { return "HTTP 404" }
func (notFoundErr) HTTPStatusCode() int { return 404 }

func TestResolveBuildNum(t *testing.T) {
	ctx := context.Background()

	t.Run("explicit number bypasses client", func(t *testing.T) {
		d := Deps{Client: fakeClient{buildsErr: errors.New("must not be called")}}
		n, err := d.ResolveBuildNum(ctx, "app", navmsg.NavBuildRef{Number: 7})
		if err != nil || n != 7 {
			t.Fatalf("got %d,%v want 7,nil", n, err)
		}
	})

	t.Run("latest from list", func(t *testing.T) {
		d := Deps{Client: fakeClient{builds: []jmodel.Build{{Number: 12}, {Number: 11}}}}
		n, err := d.ResolveBuildNum(ctx, "app", navmsg.NavBuildRef{})
		if err != nil || n != 12 {
			t.Fatalf("got %d,%v want 12,nil", n, err)
		}
	})

	t.Run("no builds", func(t *testing.T) {
		d := Deps{Client: fakeClient{}}
		if _, err := d.ResolveBuildNum(ctx, "app", navmsg.NavBuildRef{}); err == nil {
			t.Fatal("expected error for empty build list")
		}
	})

	t.Run("client error wrapped", func(t *testing.T) {
		d := Deps{Client: fakeClient{buildsErr: errors.New("boom")}}
		_, err := d.ResolveBuildNum(ctx, "app", navmsg.NavBuildRef{})
		if err == nil || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("expected wrapped boom, got %v", err)
		}
	})
}

func TestEnrichBranchNotFound(t *testing.T) {
	ctx := context.Background()
	nc := navmsg.NavigationContext{ProjectName: "app", BranchName: "feature%2Fx"}

	t.Run("non-404 passes through", func(t *testing.T) {
		d := Deps{Client: fakeClient{}}
		orig := errors.New("plain")
		if got := d.EnrichBranchNotFound(ctx, nc, orig); got != orig {
			t.Fatalf("expected original error, got %v", got)
		}
	})

	t.Run("no branch passes through", func(t *testing.T) {
		d := Deps{Client: fakeClient{}}
		orig := notFoundErr{}
		bare := navmsg.NavigationContext{ProjectName: "app"}
		if got := d.EnrichBranchNotFound(ctx, bare, orig); !errors.Is(got, orig) {
			t.Fatalf("expected original error, got %v", got)
		}
	})

	t.Run("404 lists available branches", func(t *testing.T) {
		d := Deps{Client: fakeClient{projectBuilds: []jmodel.ProjectBuild{
			{BranchName: "main"}, {BranchName: "develop"}, {BranchName: "main"},
		}}}
		err := d.EnrichBranchNotFound(ctx, nc, notFoundErr{})
		if err == nil {
			t.Fatal("expected enriched error")
		}
		msg := err.Error()
		if !strings.Contains(msg, "available branches") ||
			!strings.Contains(msg, "develop") || !strings.Contains(msg, "main") {
			t.Fatalf("missing branch list: %s", msg)
		}
	})

	t.Run("404 but project lookup fails returns original", func(t *testing.T) {
		orig := notFoundErr{}
		d := Deps{Client: fakeClient{projectErr: errors.New("nope")}}
		if got := d.EnrichBranchNotFound(ctx, nc, orig); !errors.Is(got, orig) {
			t.Fatalf("expected original error on lookup failure, got %v", got)
		}
	})
}
