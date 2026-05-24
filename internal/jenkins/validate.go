package jenkins

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// ValidationIssue, ValidationResult, FormatErrorformat live in internal/domain/jmodel.

// ValidateJenkinsfile POSTs the script to the controller's pipeline-model-converter
// /validate endpoint. Declarative-pipeline only (the converter rejects pure
// scripted pipelines with a recognisable error message — surfaced as a single
// issue rather than a transport error).
func (c *Client) ValidateJenkinsfile(ctx context.Context, content string) (jmodel.ValidationResult, error) {
	form := url.Values{}
	form.Set("jenkinsfile", content)

	endpoint := c.baseURL + "/pipeline-model-converter/validate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return jmodel.ValidationResult{}, fmt.Errorf("validate: build request: %w", err)
	}
	req.SetBasicAuth(c.username, c.token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// CSRF crumb — the validate endpoint historically required it.
	if cr, _ := c.getCrumb(ctx); cr.Field != "" {
		req.Header.Set(cr.Field, cr.Value)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return jmodel.ValidationResult{}, fmt.Errorf("validate: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return jmodel.ValidationResult{}, fmt.Errorf("validate: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return jmodel.ValidationResult{Raw: string(body)},
			fmt.Errorf("validate: HTTP %d", resp.StatusCode)
	}
	return parseValidateResponse(string(body)), nil
}

// parseValidateResponse classifies the converter's plaintext response.
//
// Success: "Jenkinsfile successfully validated."
// Failure: "Errors encountered validating Jenkinsfile:\nWorkflowScript: 5: <msg>\n..."
//
//	or "Jenkinsfile content '...' did not contain a Jenkinsfile" etc.
func parseValidateResponse(s string) jmodel.ValidationResult {
	s = strings.TrimSpace(s)
	if s == "" {
		return jmodel.ValidationResult{OK: true, Raw: s}
	}
	if strings.HasPrefix(s, "Jenkinsfile successfully validated") {
		return jmodel.ValidationResult{OK: true, Raw: s}
	}

	var issues []jmodel.ValidationIssue
	// Each structured error from pipeline-model-converter spans up to three
	// lines: the message (which carries Line via prefix or @-suffix form), an
	// echo of the offending source line, and a caret pointing at the column.
	// Only the first carries a Line; the echo + caret would otherwise surface
	// as standalone freeform "issues" that clutter the UI. Track whether the
	// previous accepted issue had a location; if so, drop subsequent
	// locationless lines as continuation.
	lastHadLocation := false
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Errors encountered") {
			continue
		}
		parsed := parseValidateLine(line)
		if parsed.Line > 0 {
			issues = append(issues, parsed)
			lastHadLocation = true
			continue
		}
		if lastHadLocation {
			continue // source echo / caret — drop
		}
		// Freeform error before any structured one (e.g. "Jenkinsfile content
		// '...' did not contain a Jenkinsfile"). Keep it as the sole issue.
		issues = append(issues, parsed)
	}
	if len(issues) == 0 {
		issues = []jmodel.ValidationIssue{{Message: s}}
	}
	return jmodel.ValidationResult{OK: false, Issues: issues, Raw: s}
}

var (
	// Prefix form: "WorkflowScript: 4: 12: Expected ..." or "Jenkinsfile: 7: ...".
	validateLineRe = regexp.MustCompile(`^(?:WorkflowScript|Jenkinsfile)\s*:\s*(\d+)(?:\s*:\s*(\d+))?\s*:?\s*(.*)$`)
	// Suffix form: "Not a valid section definition: \"foo\". ... @ line 7, column 1."
	// The pipeline-model-converter emits this for declarative validation errors
	// — the line/column travels at the *end* of the message rather than the
	// start, so the prefix regex above misses them and the issues would land
	// without a Line, hiding them from the gutter / n/N navigation.
	validateLineSuffixRe = regexp.MustCompile(`\s*@\s*line\s+(\d+),\s*column\s+(\d+)\.?\s*$`)
)

func parseValidateLine(line string) jmodel.ValidationIssue {
	if m := validateLineRe.FindStringSubmatch(line); m != nil {
		iss := jmodel.ValidationIssue{Message: strings.TrimSpace(m[3])}
		if n, err := atoiSafe(m[1]); err == nil {
			iss.Line = n
		}
		if m[2] != "" {
			if n, err := atoiSafe(m[2]); err == nil {
				iss.Col = n
			}
		}
		return iss
	}
	if loc := validateLineSuffixRe.FindStringSubmatchIndex(line); loc != nil {
		iss := jmodel.ValidationIssue{Message: strings.TrimSpace(line[:loc[0]])}
		if n, err := atoiSafe(line[loc[2]:loc[3]]); err == nil {
			iss.Line = n
		}
		if n, err := atoiSafe(line[loc[4]:loc[5]]); err == nil {
			iss.Col = n
		}
		return iss
	}
	return jmodel.ValidationIssue{Message: line}
}

func atoiSafe(s string) (int, error) {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, err
	}
	return n, nil
}
