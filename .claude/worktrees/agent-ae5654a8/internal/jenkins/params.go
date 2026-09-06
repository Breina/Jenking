package jenkins

import (
	"context"
	"encoding/json"
	"fmt"
)

// GetJobParameters fetches the parameter definitions for a job.
// Returns an empty slice (not nil) for non-parameterized jobs.
func (c *Client) GetJobParameters(ctx context.Context, jobPath string) ([]ParameterDefinition, error) {
	path := JobPathToURL(jobPath) + "/api/json?tree=property[parameterDefinitions[name,type,defaultParameterValue[value],description,choices]]"

	data, err := c.get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("get job parameters: %w", err)
	}

	var resp jsonJobDetail
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing job parameters: %w", err)
	}

	var params []ParameterDefinition
	for _, prop := range resp.Property {
		for _, pd := range prop.ParameterDefinitions {
			def := ""
			if pd.DefaultParameterValue != nil {
				def = fmt.Sprintf("%v", pd.DefaultParameterValue.Value)
			}
			params = append(params, ParameterDefinition{
				Name:        pd.Name,
				Type:        parseParamType(pd.Type),
				Default:     def,
				Description: pd.Description,
				Choices:     pd.Choices,
			})
		}
	}

	if params == nil {
		params = []ParameterDefinition{}
	}
	return params, nil
}
