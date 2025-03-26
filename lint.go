package helmlint

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

// TODO:
// - Better ignore statements? Inject into existing chart?
// - Nested validation
// - Structured comments

func Lint(t *testing.T, args ...Option) {
	opts := &options{}
	for _, opt := range args {
		opt(opts)
	}
	if err := opts.Finalize(); err != nil {
		t.Fatalf("finalizing configuration: %s", err)
	}

	// Setup
	var grp errgroup.Group
	grp.SetLimit(opts.Concurrency)
	dir := createTempDir(t, opts)

	// Copy the chart to the tempdir so we can mutate is safely
	chartDir := filepath.Join(dir, "chart")
	err := exec.Command("cp", "-r", opts.ChartDir, chartDir).Run()
	if err != nil {
		t.Fatalf("copying chart: %s", err)
	}

	// Inject comments into every conditional branch of the chart.
	// We will search for these comments later to determine test coverage.
	// Essentially a hack to avoid writing a real parser for YAML + Go templates.
	comments, err := injectComments(chartDir, &grp)
	if err != nil {
		t.Fatalf("injecting comments: %s", err)
	}

	resultsDir := filepath.Join(dir, "results")
	outputDirs := renderChart(t, opts, &grp, chartDir, resultsDir)

	ids, err := discoverComments(resultsDir, &grp)
	if err != nil {
		t.Errorf("discovering comments: %s", err)
	}

	verifyCoverage(t, &grp, comments, ids)
	runConftest(t, opts, &grp, outputDirs)
}

func createTempDir(t *testing.T, opts *options) string {
	dir, err := os.MkdirTemp("", "")
	if err != nil {
		t.Fatalf("creating tempdir: %s", err)
	}

	t.Cleanup(func() {
		if opts.Preserve {
			t.Logf("preserving temporary directory: %s", dir)
			return
		}
		err := os.RemoveAll(dir)
		if err != nil {
			t.Errorf("unable to clean up tempdir: %s", err)
		}
	})

	return dir
}

var conditionalRegex = regexp.MustCompile(`{{-?\s*if`)

func injectComments(dir string, grp *errgroup.Group) (map[string]string, error) {
	lock := sync.Mutex{}
	comments := make(map[string]string)

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".yaml") {
			return nil
		}

		grp.Go(func() error {
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}

			lines := strings.Split(string(content), "\n")
			for i, line := range lines {
				if !conditionalRegex.MatchString(line) || strings.Contains(line, "helmlint:ignore") {
					continue
				}
				id := uuid.NewString()
				indentation := strings.Repeat(" ", findIndentation(lines, i))

				lock.Lock()
				comments[id] = strings.TrimSpace(line)
				lines[i] = fmt.Sprintf("%s\n%s# helmlint: %s", line, indentation, id)
				lock.Unlock()
			}

			return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
		})

		return nil
	})
	if err != nil {
		return nil, err
	}

	return comments, grp.Wait()
}

func findIndentation(lines []string, start int) int {
	for i := start; i >= 0; i-- {
		if strings.HasSuffix(strings.TrimSpace(lines[i]), "|") {
			prevIndent := len(lines[i]) - len(strings.TrimLeft(lines[i], " "))
			return prevIndent + 2
		}
	}
	return len(lines[start]) - len(strings.TrimLeft(lines[start], " "))
}

func renderChart(t *testing.T, opts *options, grp *errgroup.Group, chartDir, resultsDir string) (outputDirs []string) {
	start := time.Now()
	defer func() {
		t.Logf("rendered the chart for every fixture in %s", time.Since(start))
	}()

	fixtures, err := os.ReadDir(opts.FixturesDir)
	if err != nil {
		t.Fatalf("reading fixtures: %s", err)
	}

	for _, fixture := range fixtures {
		if fixture.IsDir() || filepath.Ext(fixture.Name()) != ".yaml" {
			continue
		}

		fixturePath := filepath.Join(opts.FixturesDir, fixture.Name())
		outputPath := filepath.Join(resultsDir, fixture.Name()[:len(fixture.Name())-len(".yaml")])
		outputDirs = append(outputDirs, outputPath)

		grp.Go(func() error {
			out, err := exec.Command("helm", "template", "--output-dir", outputPath, "--values", fixturePath, chartDir).CombinedOutput()
			if err != nil {
				return fmt.Errorf("rendering chart with fixture %q: %s", fixture.Name(), string(out))
			}
			return nil
		})
	}

	if err := grp.Wait(); err != nil {
		t.Fatalf("rendering chart: %s", err)
	}
	return
}

func discoverComments(dir string, grp *errgroup.Group) ([]string, error) {
	const prefix = "# helmlint: "
	ids := []string{}

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".yaml") {
			return nil
		}

		grp.Go(func() error {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()

			r := bufio.NewScanner(file)
			for r.Scan() {
				line := r.Text()
				if strings.Contains(line, prefix) {
					ids = append(ids, strings.TrimPrefix(strings.TrimSpace(line), prefix))
				}
			}

			return nil
		})

		return nil
	})
	if err != nil {
		return nil, err
	}

	return ids, grp.Wait()
}

func verifyCoverage(t *testing.T, grp *errgroup.Group, comments map[string]string, ids []string) {
	for id, def := range comments {
		grp.Go(func() error {
			if slices.Contains(ids, id) {
				return nil
			}
			t.Errorf("Branch was not found in the rendered chart output:\n  %s", def)
			return errors.New("branch not found")
		})
	}
	grp.Wait()
}

func runConftest(t *testing.T, opts *options, grp *errgroup.Group, outputDirs []string) {
	for _, dir := range outputDirs {
		grp.Go(func() error {
			out, err := exec.Command("conftest", "test", "--policy", opts.PoliciesDir, dir).CombinedOutput()
			if err != nil {
				if len(out) == 0 {
					out = []byte(err.Error())
				}
				t.Errorf("Conftest failure (%s):\n%s", filepath.Base(dir), string(out))
				return err
			}
			t.Logf("Conftest output (%s):\n%s", filepath.Base(dir), string(out))
			return nil
		})
	}
	grp.Wait()
}
