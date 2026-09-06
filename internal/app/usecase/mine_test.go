package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

func TestFilterRunningMine(t *testing.T) {
	builds := []jmodel.UserBuild{
		{JobPath: "a", Build: jmodel.Build{Number: 1, TriggeredBy: "brecht"}},
		{JobPath: "b", Build: jmodel.Build{Number: 2, TriggeredBy: "someone"}},
		{JobPath: "c", Build: jmodel.Build{Number: 3, Cause: "Started by GitLab push by Brecht Derwael"}},
	}
	d := Deps{
		Client:       fakeClient{whoami: &jmodel.User{ID: "brecht"}},
		GitUsernames: []string{"Brecht Derwael"},
	}
	got, err := d.FilterRunningMine(context.Background(), builds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 builds (userId + git-cause match), got %d", len(got))
	}
	if got[0].Number != 1 || got[1].Number != 3 {
		t.Errorf("unexpected builds kept: %+v", got)
	}
}

func TestFilterBuildsMine_NoGitUsernames(t *testing.T) {
	builds := []jmodel.Build{
		{Number: 1, TriggeredBy: "brecht"},
		{Number: 2, Cause: "Started by GitLab push by Brecht Derwael"},
	}
	d := Deps{Client: fakeClient{whoami: &jmodel.User{ID: "brecht"}}}
	got, err := d.FilterBuildsMine(context.Background(), builds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Without configured git usernames, only the direct userId trigger matches.
	if len(got) != 1 || got[0].Number != 1 {
		t.Fatalf("expected only build #1, got %+v", got)
	}
}

func TestFilterMine_WhoAmIError(t *testing.T) {
	d := Deps{Client: fakeClient{whoamiErr: errors.New("boom")}}
	if _, err := d.FilterRunningMine(context.Background(), nil); err == nil {
		t.Fatal("expected error when WhoAmI fails")
	}
}
