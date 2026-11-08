package cmd

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"lukechampine.com/blake3"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// verifiedDir is where rule promote keeps its records, relative to the
// project. It sits under .strspc/, which check and verify never read.
const verifiedDir = ".strspc/verified"

// verifyRecord is the evidence a promotion from Published to Implemented
// rested on, written to .strspc/verified/<RULE-ID>.json to be committed
// with the change to rules/. A promotion with --skip-verify is recorded as
// skipped, with no verdict.
type verifyRecord struct {
	RuleID   string `json:"rule_id"`
	Revision int    `json:"revision"`
	// BodyHash is the blake3 hash of the rule text promoted.
	BodyHash string `json:"body_hash"`
	// PromotedAt is the rule set timestamp the promotion wrote.
	PromotedAt    string `json:"promoted_at"`
	EntityVersion string `json:"entity_version"`
	Skipped       bool   `json:"skipped,omitempty"`

	// The verify run: what it read and with what.
	Scope         string  `json:"scope,omitempty"`
	Files         int     `json:"files,omitempty"`
	FilesHash     string  `json:"files_hash,omitempty"`
	Provider      string  `json:"provider,omitempty"`
	Model         string  `json:"model,omitempty"`
	Effort        string  `json:"effort,omitempty"`
	AuditPrompt   string  `json:"audit_prompt,omitempty"`
	MinConfidence float64 `json:"min_confidence,omitempty"`

	// The verdict.
	Verdict     string           `json:"verdict,omitempty"`
	Confidence  float64          `json:"confidence,omitempty"`
	Explanation string           `json:"explanation,omitempty"`
	Evidence    []verifyEvidence `json:"evidence,omitempty"`
}

// newVerifyRecord builds the record of a promotion of rule (as promoted,
// in state I) to entity version, at timestamp. report and vr are the
// verify run and its verdict, both nil when verification was skipped.
func newVerifyRecord(rule *entity.Rule, version, timestamp string, report *verifyOutput, vr *verifyRule, minConfidence float64) verifyRecord {
	sum := blake3.Sum256([]byte(rule.Body))
	rec := verifyRecord{
		RuleID:        rule.ID,
		Revision:      rule.Revision,
		BodyHash:      "blake3:" + hex.EncodeToString(sum[:]),
		PromotedAt:    timestamp,
		EntityVersion: version,
		Skipped:       vr == nil,
	}
	if report != nil {
		rec.Scope = report.Scope
		rec.Files = report.Estimate.Files
		rec.FilesHash = report.FilesHash
		rec.Provider, rec.Model, rec.Effort, rec.AuditPrompt = report.Provider, report.Model, report.Effort, report.AuditPrompt
	}
	if vr != nil {
		rec.MinConfidence = minConfidence
		rec.Verdict, rec.Confidence, rec.Explanation, rec.Evidence = vr.Verdict, vr.Confidence, vr.Explanation, vr.Evidence
	}
	return rec
}

// errNoStrspcDir reports a project without .strspc/, where no record is
// written rather than a directory created.
var errNoStrspcDir = errors.New("the project has no .strspc/ directory")

// writeVerifyRecord writes rec to .strspc/verified/<RULE-ID>.json under
// project and returns that path, relative to the project and with forward
// slashes. A project without .strspc/ gets no record (errNoStrspcDir).
func writeVerifyRecord(project string, rec *verifyRecord) (string, error) {
	if fi, err := os.Stat(filepath.Join(project, ".strspc")); err != nil || !fi.IsDir() {
		return "", errNoStrspcDir
	}
	dir := filepath.Join(project, filepath.FromSlash(verifiedDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("writing verify record: %w", err)
	}
	data, err := marshalJSON(rec)
	if err != nil {
		return "", fmt.Errorf("writing verify record: %w", err)
	}
	// The rule ID names the file: an EUID and three digits, as
	// entityIDFromRuleID and resolveEntityTarget checked.
	name := rec.RuleID + ".json"
	if err := replaceFile(filepath.Join(dir, name), data, "verify record"); err != nil {
		return "", err
	}
	return path.Join(verifiedDir, name), nil
}
