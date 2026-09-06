package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Breina/Jenking/internal/app/view"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/jenkins"
)

// loginPollInterval and loginTimeout bound how long we wait for the user to
// finish the SSO round-trip in their browser.
const (
	loginPollInterval = 2 * time.Second
	loginTimeout      = 3 * time.Minute
)

// loginURL is the security realm entry point that starts the SSO flow and
// returns the browser to the Jenkins dashboard afterwards.
func loginURL(baseURL string) string {
	return strings.TrimSuffix(baseURL, "/") + "/securityRealm/commenceLogin?from=" + url.QueryEscape("/")
}

// whoAmI probes the connection with a bounded timeout, so the login wait loop
// can keep polling without inheriting a single short-lived deadline.
func whoAmI(ctx context.Context) (*jmodel.User, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return cs.client.WhoAmI(ctx)
}

// ensureAuthenticated resolves the current user, walking the user through an
// interactive SSO login when Jenkins refuses our credentials. It returns the
// user once authentication succeeds.
func ensureAuthenticated(ctx context.Context) (*jmodel.User, error) {
	active := cs.cfg.ActiveContext()
	user, err := whoAmI(ctx)
	if err == nil {
		return user, nil
	}
	if !errors.Is(err, jenkins.ErrLoginRequired) {
		return nil, fmt.Errorf("connecting to Jenkins at %s (user: %s): %w", active.URL, active.Username, err)
	}
	if err := interactiveLogin(ctx, active.URL); err != nil {
		return nil, err
	}
	user, err = whoAmI(ctx)
	if err != nil {
		return nil, fmt.Errorf("connecting to Jenkins at %s (user: %s): %w", active.URL, active.Username, err)
	}
	return user, nil
}

// interactiveLogin opens the SSO login page in the system browser and polls
// until Jenkins accepts our credentials again.
func interactiveLogin(ctx context.Context, baseURL string) error {
	target := loginURL(baseURL)
	fmt.Printf("Jenkins at %s needs an SSO login.\nOpening %s in your browser…\n", baseURL, target)
	view.OpenURL(target)
	fmt.Println("Waiting for the login to complete (Ctrl-C to abort)…")

	deadline := time.Now().Add(loginTimeout)
	for {
		if _, err := whoAmI(ctx); err == nil {
			fmt.Println("Logged in.")
			return nil
		} else if !errors.Is(err, jenkins.ErrLoginRequired) {
			return fmt.Errorf("connecting to Jenkins at %s: %w", baseURL, err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for SSO login at %s", target)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(loginPollInterval):
		}
	}
}

func newLoginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Log in to Jenkins via the browser (SSO)",
		Long: `Open the Jenkins SSO login page in the system browser and wait until
the session is established.

Examples:
  jenking login
  jenking login --context ontwikkel`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			active := cs.cfg.ActiveContext()
			if user, err := cs.client.WhoAmI(cmd.Context()); err == nil {
				fmt.Printf("Already logged in to %s as %s\n", active.URL, user.FullName)
				return nil
			} else if !errors.Is(err, jenkins.ErrLoginRequired) {
				return fmt.Errorf("connecting to Jenkins at %s (user: %s): %w", active.URL, active.Username, err)
			}
			return interactiveLogin(cmd.Context(), active.URL)
		},
	}
}
