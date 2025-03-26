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
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

type T interface {
	Logf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
	Fatalf(format string, args ...interface{})
	Cleanup(func())
	FailNow()
}

func Lint(t T, args ...Option) {
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
	chartDir := copyChart(t, opts.ChartDir, dir)

	// Inject ids into every conditional branch of the chart.
	// We will search for these ids later to determine test coverage.
	// Essentially a hack to avoid writing a real parser for YAML + Go templates.
	ids, err := injectComments(chartDir, &grp)
	if err != nil {
		t.Fatalf("injecting comments: %s", err)
	}

	resultsDir := filepath.Join(dir, "results")
	outputDirs := renderChart(t, opts, &grp, chartDir, resultsDir)

	seenIDs, err := discoverComments(resultsDir, &grp)
	if err != nil {
		t.Errorf("discovering comments: %s", err)
	}

	if opts.WriteExceptions {
		err := injectExceptions(t, opts, ids, seenIDs)
		if err != nil {
			t.Errorf("injecting exceptions: %s", err)
		}
	} else {
		verifyCoverage(t, &grp, ids, seenIDs)
	}

	walkRenderedChart(t, opts, &grp, dir, outputDirs)
}

func createTempDir(t T, opts *options) string {
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

func copyChart(t T, dir, tempDir string) string {
	chartDir := filepath.Join(tempDir, "chart")
	err := exec.Command("cp", "-r", dir, chartDir).Run()
	if err != nil {
		t.Fatalf("copying chart: %s", err)
	}
	return chartDir
}

var conditionalRegex = regexp.MustCompile(`{{-?\s*if`)

type conditionalDeclRef struct {
	File string // relative to chart directory
	Line int
	Expr string
}

func injectComments(dir string, grp *errgroup.Group) (map[string]*conditionalDeclRef, error) {
	lock := sync.Mutex{}
	comments := make(map[string]*conditionalDeclRef)

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
				if !conditionalRegex.MatchString(line) ||
					strings.Contains(lines[i], "helmlint:ignore") ||
					(i > 0 && strings.Contains(lines[i-1], "helmlint:ignore")) {
					continue
				}
				id := uuid.NewString()
				indentation := strings.Repeat(" ", findIndentation(lines, i))

				lock.Lock()
				comments[id] = &conditionalDeclRef{
					File: path[len(dir)+1:],
					Line: i,
					Expr: strings.TrimSpace(line),
				}
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

func renderChart(t T, opts *options, grp *errgroup.Group, chartDir, resultsDir string) (outputDirs []string) {
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

func discoverComments(dir string, grp *errgroup.Group) (ids []string, err error) {
	const prefix = "# helmlint: "
	var lock sync.Mutex

	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
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
					lock.Lock()
					ids = append(ids, strings.TrimPrefix(strings.TrimSpace(line), prefix))
					lock.Unlock()
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

func verifyCoverage(t T, grp *errgroup.Group, ids map[string]*conditionalDeclRef, seenIDs []string) {
	for id, def := range ids {
		grp.Go(func() error {
			if slices.Contains(seenIDs, id) {
				return nil
			}
			t.Errorf("Branch was not found in the rendered chart output:\n  (%s:%d): %s", def.File, def.Line, def.Expr)
			return errors.New("branch not found")
		})
	}
	grp.Wait()
}

func injectExceptions(t T, opts *options, ids map[string]*conditionalDeclRef, seenIDs []string) error {
	byFile := map[string][]*conditionalDeclRef{}
	for id, def := range ids {
		if slices.Contains(seenIDs, id) {
			continue
		}
		byFile[def.File] = append(byFile[def.File], def)
	}

	for relPath, defs := range byFile {
		path := filepath.Join(opts.ChartDir, relPath)
		file, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		lines := strings.Split(string(file), "\n")
		for _, def := range defs {
			indentation := strings.Repeat(" ", findIndentation(lines, def.Line))
			lines[def.Line] = fmt.Sprintf("%s{{/* helmlint:ignore */}}", indentation) + "\n" + lines[def.Line]
		}

		err = os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
		if err != nil {
			return err
		}
		t.Logf("Injected exceptions into %s", relPath)
	}

	return nil
}

func walkRenderedChart(t T, opts *options, grp *errgroup.Group, dir string, outputDirs []string) {
	recursionDir := filepath.Join(dir, "recursions")
	for _, dir := range outputDirs {
		grp.Go(func() error {
			return runConftest(t, opts.PoliciesDir, dir)
		})

		for _, fn := range opts.Visitors {
			grp.Go(func() error {
				fn(t, dir)
				return nil
			})
		}

		// "recurse" into resources contained within rendered resources
		for i, rule := range opts.Recursions {
			target := filepath.Join(recursionDir, fmt.Sprintf("%s-recursion-%d", filepath.Base(dir), i))
			if err := os.MkdirAll(target, 0755); err != nil {
				t.Errorf("unable to create recursion directory: %s", err)
				continue
			}

			grp.Go(func() error {
				if err := rule.Fn(dir, target); err != nil {
					t.Errorf("error in recursion function: %s", err)
					return err
				}
				files, _ := os.ReadDir(target)
				if len(files) == 0 {
					return nil // nothing to do
				}
				return runConftest(t, rule.Opts.PoliciesDir, target)
			})
		}
	}
	grp.Wait() // no need to handle error - the goroutines will fail the test as needed
}

func runConftest(t T, policiesDir, dir string) error {
	out, err := exec.Command("conftest", "test", "--policy", policiesDir, dir).CombinedOutput()
	if err == nil {
		t.Logf("Conftest output (%s):\n%s", filepath.Base(dir), string(out))
		return nil
	}
	if len(out) == 0 {
		out = []byte(err.Error())
	}
	t.Errorf("Conftest failure (%s):\n%s", filepath.Base(dir), string(out))
	return err
}
