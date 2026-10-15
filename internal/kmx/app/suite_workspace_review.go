package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	suiteoras "github.com/kaimahi-agents/kaimahi/internal/kmx/agentsuite/oras"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/agentsuite/workspace"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
	agentruntime "github.com/kaimahi-agents/kaimahi/internal/kmx/runtime"
)

// Review retains the packaged selection and its source fingerprint, not proof
// of execution. No cluster configuration, placement, or Task is involved.
type suiteWorkspaceReview struct {
	modified  time.Time
	root      string
	prepared  *workspace.Prepared
	suite     *workspace.NativeSuite
	inventory []suitePassingInventoryCase
	image     *workspace.ImageSuite
}

// ShowSuiteWorkspace renders only authored inventory, never live placement or
// evaluated state. Preparation uses the existing private artifact cache.
func ShowSuiteWorkspace(ctx context.Context, root, output string, out io.Writer) error {
	a := &App{}
	r, err := a.reviewSuiteWorkspace(ctx, root)
	if err != nil {
		return err
	}
	view := struct {
		Workspace, Suite, Member, Runtime, Model, Digest string
		Cases                                            []string
	}{Workspace: r.root, Suite: r.name(), Member: r.member(), Runtime: r.profile(), Model: r.model(), Digest: r.digest()}
	for _, c := range r.inventory {
		view.Cases = append(view.Cases, c.Case.ID)
	}
	if output == "json" {
		return json.NewEncoder(out).Encode(view)
	}
	_, err = fmt.Fprintf(out, "Suite: %s\nMember: %s (%s)\nWorkspace: %s\nArtifact: %s\nModel: %s\nCases: %s\nEvidence: not checked\n", safeTerminal(view.Suite), safeTerminal(view.Member), view.Runtime, safeTerminal(view.Workspace), view.Digest, safeTerminal(view.Model), safeTerminal(strings.Join(view.Cases, ", ")))
	return err
}

func (r *suiteWorkspaceReview) name() string {
	if r.image != nil {
		return r.image.Name
	}
	return r.suite.Name
}
func (r *suiteWorkspaceReview) member() string {
	if r.image != nil {
		return r.image.MemberID
	}
	return r.suite.MemberID
}
func (r *suiteWorkspaceReview) profile() string {
	if r.image != nil {
		return "image-http"
	}
	return r.suite.Runtime.ID
}
func (r *suiteWorkspaceReview) model() string {
	if r.image != nil {
		return "deployment-bound"
	}
	return r.suite.Model
}
func (r *suiteWorkspaceReview) instructions() []byte {
	if r.image != nil {
		return r.image.Instructions
	}
	return r.suite.Instructions
}

func (a *App) reviewSuiteWorkspace(ctx context.Context, root string) (*suiteWorkspaceReview, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("select an explicit authored workspace root; kmx create authors offline separately")
	}
	resolved, err := resolveSuiteWorkspace(root)
	if err != nil {
		return nil, err
	}
	state, err := config.StateDir()
	if err != nil {
		return nil, err
	}
	prepared, err := workspace.Prepare(ctx, resolved, filepath.Join(state, "suites"))
	if err != nil {
		return nil, err
	}
	suite, err := workspace.Load(ctx, prepared)
	if err != nil {
		artifact, readErr := suiteoras.ReadArtifactLayout(ctx, prepared.Path, prepared.Descriptor.Digest.String())
		if readErr != nil {
			return nil, err
		}
		image, imageErr := workspace.LoadImageArtifact(ctx, artifact)
		if imageErr != nil {
			return nil, err
		}
		inventory, _, imageErr := imageInventory(image)
		if imageErr != nil {
			return nil, imageErr
		}
		review := &suiteWorkspaceReview{root: resolved, prepared: prepared, image: image, inventory: inventory}
		review.modified = suiteSourceModified(ctx, review)
		return review, nil
	}
	if !suiteNativeOrkaInput(suite) {
		return nil, fmt.Errorf("console workspace review supports native Orka only; kagent evaluation is unavailable")
	}
	inventory, _, err := suitePassingInventory(suite)
	if err != nil {
		return nil, err
	}
	review := &suiteWorkspaceReview{root: resolved, prepared: prepared, suite: suite, inventory: inventory}
	review.modified = suiteSourceModified(ctx, review)
	if err := review.checkCurrent(ctx); err != nil {
		return nil, err
	}
	return review, nil
}

func (r *suiteWorkspaceReview) checkCurrent(ctx context.Context) error {
	if r == nil {
		return fmt.Errorf("workspace review required")
	}
	return workspace.CheckCurrent(ctx, r.root, r.prepared)
}

// Large public operands are explicitly marked as previews. Every case and every
// assertion still has its identity and definition digest displayed.
func suiteReviewPreview(value string) string {
	const limit = 2048
	if len(value) <= limit {
		return strconv.Quote(value)
	}
	prefix := value[:limit]
	for !utf8.ValidString(prefix) {
		prefix = prefix[:len(prefix)-1]
	}
	return strconv.Quote(prefix) + fmt.Sprintf(" [preview: %d bytes total; inspect full authored file]", len(value))
}

func (r *suiteWorkspaceReview) sourceLines(width int) []string {
	t := newSuiteConsoleTheme()
	title := "WORKSPACE / SOURCE REVIEW"
	if r.image != nil {
		title = "IMAGE WORKSPACE / SOURCE REVIEW"
	}
	rows := reviewIndented(t.accent.Render(title), width, 0)
	rows = append(rows, reviewIndented(t.muted.Render("Review only · no Tasks or model requests"), width, 0)...)
	for _, field := range [][2]string{{"Root", r.root}, {"Member", r.member()}, {"Execution", r.profile()}, {"Model", r.model()}, {"Suite digest", r.digest()}} {
		rows = append(rows, reviewField(field[0], field[1], width, 2)...)
	}
	if r.suite != nil {
		rows = append(rows, reviewField("Member digest", r.suite.MemberDigest, width, 2)...)
		rows = append(rows, reviewField("Instructions digest", r.suite.InstructionsDigest, width, 2)...)
	}
	rows = append(rows, "", t.field.Render("Instructions (public content): |"))
	rows = append(rows, reviewBlock(string(r.instructions()), width, 2)...)
	rows = append(rows, "", t.field.Render("Evaluation:"))
	rows = append(rows, reviewField("Complete inventory", fmt.Sprintf("%d cases (each reviewed next)", len(r.inventory)), width, 2)...)
	for _, note := range []string{"Cases are public content, not private recorded conversations.", "Scaffold examples are not quality proof; author cases appropriate to these instructions.", "This review executes no Tasks or model requests."} {
		rows = append(rows, reviewIndented(t.muted.Render(note), width, 2)...)
	}
	if r.image != nil {
		rows = append(rows, reviewIndented(t.muted.Render("Concrete inference comes from environment bindings. Publication builds an immutable image candidate; evaluation places a fresh workload and executes every case."), width, 2)...)
	}
	return rows
}

func reviewIndented(text string, width, indent int) []string {
	indent = min(indent, max(0, width-1))
	prefix := strings.Repeat(" ", indent)
	lines := strings.Split(ansi.Hardwrap(text, max(1, width-indent), true), "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return lines
}

func reviewField(label, value string, width, indent int) []string {
	return reviewIndented(newSuiteConsoleTheme().property(label, value), width, indent)
}

func reviewBlock(value string, width, indent int) []string {
	const limit = 2048
	t := newSuiteConsoleTheme()
	size := len(value)
	truncated := size > limit
	if truncated {
		value = value[:limit]
		for !utf8.ValidString(value) {
			value = value[:len(value)-1]
		}
	}
	// Preserve authored newlines/indentation, never terminal controls. This is
	// a structured preview, not executable YAML or a replacement for source files.
	value = strings.ReplaceAll(value, "\r\n", "\n")
	var rows []string
	for _, line := range strings.Split(value, "\n") {
		line = strings.ReplaceAll(safeTerminal(line), "\t", "  ")
		style := t.text
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			style = t.accent
		}
		rows = append(rows, reviewIndented(style.Render(line), width, indent)...)
	}
	if truncated {
		rows = append(rows, reviewIndented(t.warning.Render(fmt.Sprintf("[preview: %d bytes total; inspect full authored file]", size)), width, indent)...)
	}
	return rows
}

func (r *suiteWorkspaceReview) digest() string {
	if r.image != nil {
		return r.image.Digest
	}
	return r.suite.Digest
}

func (r *suiteWorkspaceReview) caseLines(index, width int) []string {
	c := r.inventory[index]
	t := newSuiteConsoleTheme()
	rows := reviewIndented(t.accent.Render(fmt.Sprintf("CASE REVIEW %d/%d · %s", index+1, len(r.inventory), safeTerminal(c.Case.ID))), width, 0)
	for _, field := range [][2]string{{"File", c.Metadata.Path}, {"Case digest", c.Metadata.CaseDigest}, {"Input digest", c.Metadata.InputDigest}} {
		rows = append(rows, reviewField(field[0], field[1], width, 2)...)
	}
	rows = append(rows, "", t.field.Render("Input (public content): |"))
	rows = append(rows, reviewBlock(c.Case.Input, width, 2)...)
	rows = append(rows, "", t.field.Render("Assertions:"))
	for _, assertion := range agentruntime.EvaluationCaseAssertions(c.Case) {
		operand := assertion.Value
		if assertion.Type == "regex" {
			operand = assertion.Pattern
		}
		rows = append(rows, reviewField("- Assertion", assertion.ID, width, 2)...)
		rows = append(rows, reviewField("Type", assertion.Type, width, 4)...)
		rows = append(rows, reviewIndented(t.field.Render("Operand: |"), width, 4)...)
		rows = append(rows, reviewBlock(operand, width, 6)...)
		rows = append(rows, reviewField("Definition digest", agentruntime.EvaluationAssertionDigest(assertion), width, 4)...)
	}
	return rows
}
