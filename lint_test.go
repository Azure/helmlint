package helmlint

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

func TestHappyPath_Simple(t *testing.T) {
	Lint(t, WithChartDir("fixtures/simple"))
}

func TestLintFailure_Simple(t *testing.T) {
	ft := &fakeT{T: t}
	Lint(ft, WithChartDir("fixtures/simple"), WithPoliciesDir("bad-policies"))
	require.Len(t, ft.Errors, 1)
	assert.Contains(t, ft.Errors[0], "simple-example must not include the forbidden label")
}

func TestHappyPath_Simple_Visitor(t *testing.T) {
	var visited atomic.Bool
	Lint(t, WithChartDir("fixtures/simple"), WithVisitor(func(t T, dir string) {
		t.Logf("visiting %s", dir)
		visited.Store(true)

		_, err := os.Stat(filepath.Join(dir, "simple/templates/deployment.yaml"))
		require.NoError(t, err)
	}))
	assert.True(t, visited.Load())
}

func TestHappyPath_Recursion(t *testing.T) {
	Lint(t,
		WithChartDir("fixtures/recursive"),
		WithRecursion(RecurseConfigmap("recursive/templates/configmap.yaml")),
	)
}

func TestLintFailure_Recursion(t *testing.T) {
	ft := &fakeT{T: t}
	Lint(ft,
		WithChartDir("fixtures/recursive"),
		WithRecursion(RecurseConfigmap("recursive/templates/configmap.yaml"), WithPoliciesDir("bad-policies")),
	)
	require.Len(t, ft.Errors, 1)
	assert.Contains(t, ft.Errors[0], "simple-example-deploy must not include the forbidden label")
}

func TestLintFailure_Recursion_TopLevel(t *testing.T) {
	ft := &fakeT{T: t}
	Lint(ft,
		WithChartDir("fixtures/recursive"),
		WithPoliciesDir("bad-policies"),
		WithRecursion(RecurseConfigmap("recursive/templates/configmap.yaml")),
	)
	require.Len(t, ft.Errors, 2)
	slices.Sort(ft.Errors)
	assert.Contains(t, ft.Errors[0], "simple-example-cm must not include the forbidden label")
	assert.Contains(t, ft.Errors[1], "simple-example-deploy must not include the forbidden label")
}

func TestLintMissingIfBranch(t *testing.T) {
	ft := &fakeT{T: t}
	Lint(ft, WithChartDir("fixtures/missing-if-branch"))
	require.Len(t, ft.Errors, 1)
	assert.Contains(t, ft.Errors[0], "Branch was not found")
}

func TestLintMissingElseBranch(t *testing.T) {
	ft := &fakeT{T: t}
	Lint(ft, WithChartDir("fixtures/missing-else-branch"))
	require.Len(t, ft.Errors, 1)
	assert.Contains(t, ft.Errors[0], "Branch was not found")
}

func TestCommentInjection(t *testing.T) {
	firstCopy := copyChart(t, "./examples/simple-chart", t.TempDir())
	secondCopy := copyChart(t, "./examples/simple-chart", t.TempDir())

	var grp errgroup.Group
	ids, err := injectComments(firstCopy, &grp)
	require.NoError(t, err)
	assert.Len(t, ids, 3)

	// All comments are found in the unrendered chart
	found, err := discoverComments(firstCopy, &grp)
	require.NoError(t, err)
	assert.Len(t, found, 3)

	// Add exceptions for some of the comments
	err = injectExceptions(t, &options{ChartDir: secondCopy}, ids, found[:2])
	require.NoError(t, err)

	ids, err = injectComments(secondCopy, &grp)
	require.NoError(t, err)
	assert.Len(t, ids, 2)

	found, err = discoverComments(secondCopy, &grp)
	require.NoError(t, err)
	assert.Len(t, found, 2)
}

func TestFindIndentation_IndentedIf(t *testing.T) {
	lines := []string{
		"  {{ if foo }}",
	}
	assert.Equal(t, 2, findIndentation(lines, 0))
}

func TestFindIndentation_UnIndentedIf(t *testing.T) {
	lines := []string{
		"{{ if foo }}",
	}
	assert.Equal(t, 0, findIndentation(lines, 0))
}

func TestFindIndentation_UnIndentedIf_MultilineStr(t *testing.T) {
	lines := []string{
		"   foo: |", // 3 spaces
		"# foobar",
		"  {{ if foo }}", // 2 spaces
	}
	assert.Equal(t, 5 /* 3 + 2 spaces */, findIndentation(lines, 2))
}

func TestCreateTempDir(t *testing.T) {
	dir := createTempDir(t, &options{})
	assert.NotEmpty(t, dir)
}

type fakeT struct {
	*testing.T
	lock   sync.Mutex
	Errors []string
}

func (f *fakeT) Errorf(format string, args ...interface{}) {
	str := fmt.Sprintf(format, args...)
	f.T.Logf("Errorf was called:\n%s", str)

	f.lock.Lock()
	f.Errors = append(f.Errors, str)
	f.lock.Unlock()
}
