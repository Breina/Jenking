package jenkins

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// ValidationIssue is one problem the Jenkins pipeline-model-converter found in
// a Jenkinsfile. Line/Col are 1-based; both are 0 when the converter didn't
// report a position (e.g. tokenisation errors that point at the whole file).
type ValidationIssue struct {
	Line    int
	Col     int
	Message string
}

// ValidationResult is the parsed response from /pipeline-model-converter/validate.
type ValidationResult struct {
	OK     bool
	Issues []ValidationIssue
	Raw    string
}

// FormatErrorformat renders the issues in a format vim's errorformat picks up
// with `%f:%l:%c: %m` (col present) and `%f:%l: %m` (col missing).
func (r ValidationResult) FormatErrorformat(file string) string {
	if len(r.Issues) == 0 {
		return ""
	}
	var b strings.Builder
	for _, iss := range r.Issues {
		if iss.Col > 0 {
			fmt.Fprintf(&b, "%s:%d:%d: %s\n", file, iss.Line, iss.Col, iss.Message)
		} else {
			line := iss.Line
			if line == 0 {
				line = 1
			}
			fmt.Fprintf(&b, "%s:%d: %s\n", file, line, iss.Message)
		}
	}
	return b.String()
}

// ValidateJenkinsfile POSTs the script to the controller's pipeline-model-converter
// /validate endpoint. Declarative-pipeline only (the converter rejects pure
// scripted pipelines with a recognisable error message — surfaced as a single
// issue rather than a transport error).
func (c *Client) ValidateJenkinsfile(ctx context.Context, content string) (ValidationResult, error) {
	form := url.Values{}
	form.Set("jenkinsfile", content)

	endpoint := c.baseURL + "/pipeline-model-converter/validate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return ValidationResult{}, fmt.Errorf("validate: build request: %w", err)
	}
	req.SetBasicAuth(c.username, c.token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// CSRF crumb — the validate endpoint historically required it.
	if cr, _ := c.getCrumb(ctx); cr.Field != "" {
		req.Header.Set(cr.Field, cr.Value)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ValidationResult{}, fmt.Errorf("validate: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ValidationResult{}, fmt.Errorf("validate: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ValidationResult{Raw: string(body)},
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
func parseValidateResponse(s string) ValidationResult {
	s = strings.TrimSpace(s)
	if s == "" {
		return ValidationResult{OK: true, Raw: s}
	}
	if strings.HasPrefix(s, "Jenkinsfile successfully validated") {
		return ValidationResult{OK: true, Raw: s}
	}

	var issues []ValidationIssue
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
		issues = []ValidationIssue{{Message: s}}
	}
	return ValidationResult{OK: false, Issues: issues, Raw: s}
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

func parseValidateLine(line string) ValidationIssue {
	if m := validateLineRe.FindStringSubmatch(line); m != nil {
		iss := ValidationIssue{Message: strings.TrimSpace(m[3])}
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
		iss := ValidationIssue{Message: strings.TrimSpace(line[:loc[0]])}
		if n, err := atoiSafe(line[loc[2]:loc[3]]); err == nil {
			iss.Line = n
		}
		if n, err := atoiSafe(line[loc[4]:loc[5]]); err == nil {
			iss.Col = n
		}
		return iss
	}
	return ValidationIssue{Message: line}
}

func atoiSafe(s string) (int, error) {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, err
	}
	return n, nil
}
