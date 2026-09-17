package usecase

import (
	"context"
	"fmt"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// UpdateBuild changes a build's display name and/or description; a nil field is
// left unchanged. Returns the build as Jenkins reports it afterwards.
func (d Deps) UpdateBuild(ctx context.Context, jobPath string, number int, displayName, description *string) (jmodel.Build, error) {
	switch {
	case displayName == nil && description == nil:
		return jmodel.Build{}, fmt.Errorf("nothing to update: set a display name and/or description")
	case displayName == nil:
		if err := d.Client.SetBuildDescription(ctx, jobPath, number, *description); err != nil {
			return jmodel.Build{}, err
		}
	case *displayName == "":
		return jmodel.Build{}, fmt.Errorf("display name cannot be empty")
	default:
		// configSubmit overwrites the description too, so carry the current one
		// over unless the caller replaces it.
		current, err := d.Client.GetBuild(ctx, jobPath, number)
		if err != nil {
			return jmodel.Build{}, err
		}
		desc := current.Description
		if description != nil {
			desc = *description
		}
		if err := d.Client.ConfigureBuild(ctx, jobPath, number, *displayName, desc); err != nil {
			return jmodel.Build{}, err
		}
	}
	updated, err := d.Client.GetBuild(ctx, jobPath, number)
	if err != nil {
		return jmodel.Build{}, err
	}
	return updated.Build, nil
}

// RebuildResult is a Rebuild's trigger outcome plus what it resubmitted.
type RebuildResult struct {
	TriggerResult
	Params map[string]string
	// Dropped names password parameters: Jenkins never exposes their values, so
	// the new build gets the job default for them unless the caller overrides.
	Dropped []string
}

// Rebuild queues a new build with the parameters an earlier build ran with;
// opt.Params override individual values. Wait/Progress behave as in Trigger.
func (d Deps) Rebuild(ctx context.Context, jobPath string, number int, opt TriggerOptions) (RebuildResult, error) {
	params, err := d.Client.GetBuildParameters(ctx, jobPath, number)
	if err != nil {
		return RebuildResult{}, err
	}
	defs, err := d.Client.GetJobParameters(ctx, jobPath)
	if err != nil {
		return RebuildResult{}, err
	}
	var dropped []string
	for _, def := range defs {
		if def.Type != jmodel.ParamTypePassword {
			continue
		}
		if _, ok := params[def.Name]; !ok {
			continue
		}
		delete(params, def.Name)
		if _, overridden := opt.Params[def.Name]; !overridden {
			dropped = append(dropped, def.Name)
		}
	}
	for k, v := range opt.Params {
		params[k] = v
	}
	opt.Params = params
	res, err := d.Trigger(ctx, jobPath, opt)
	return RebuildResult{TriggerResult: res, Params: params, Dropped: dropped}, err
}
