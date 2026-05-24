package logscore

import "testing"

func TestScoreImportance_EmptyLine(t *testing.T) {
	scores, line := ScoreImportance("")
	if scores != nil {
		t.Errorf("empty line: want nil scores, got %v", scores)
	}
	if line != 0.3 {
		t.Errorf("empty line: want default score 0.3, got %v", line)
	}
}

func TestScoreImportance_ErrorOutranksInfo(t *testing.T) {
	_, errScore := ScoreImportance("ERROR: connection refused")
	_, infoScore := ScoreImportance("just some plain text here")
	if errScore <= infoScore {
		t.Errorf("ERROR line (%.3f) should outscore plain text (%.3f)", errScore, infoScore)
	}
}

func TestScoreImportance_PipelineBookkeepingDimmed(t *testing.T) {
	scores, line := ScoreImportance("[Pipeline] echo")
	if line > 0.2 {
		t.Errorf("[Pipeline] bookkeeping should be very dim, got %.3f", line)
	}
	for i, s := range scores {
		if s > 0.05 {
			t.Errorf("bookkeeping char %d (%q): score %.3f > 0.05", i, "[Pipeline] echo"[i:i+1], s)
		}
	}
}

func TestScoreImportance_StageBannerHighlightsStageName(t *testing.T) {
	line := "[Pipeline] { (Sonar scan)"
	scores, _ := ScoreImportance(line)
	// "Sonar scan" lives between the parens; expect those runes to be bright.
	runes := []rune(line)
	start, end := -1, -1
	for i, r := range runes {
		if r == '(' && start == -1 {
			start = i + 1
		}
		if r == ')' && end == -1 {
			end = i
		}
	}
	if start < 0 || end < 0 {
		t.Fatalf("parse anchors not found in %q", line)
	}
	for i := start; i < end; i++ {
		if scores[i] < 0.9 {
			t.Errorf("stage-name char %d: want >=0.9, got %.3f", i, scores[i])
		}
	}
}

func TestScoreImportance_InfoBracketSuppressesWholeLineBoost(t *testing.T) {
	// [INFO] prefix in first 30 chars should suppress whole-line boost even
	// though the line contains "error" later.
	_, withInfo := ScoreImportance("[INFO] no real error here, just chatty logging")
	_, plain := ScoreImportance("[ERROR] kaboom database connection")
	if withInfo >= plain {
		t.Errorf("[INFO] line (%.3f) should not outscore [ERROR] line (%.3f)", withInfo, plain)
	}
}
