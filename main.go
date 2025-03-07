package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

func main() {
	var (
		fixtureDir = flag.String("fixtures-dir", "fixtures", "Directory containing fixtures e.g. values files to be tested. Relative to chart directory.")
		preserve   = flag.Bool("preserve", false, "Log the temporary directory instead of deleting it")
	)
	flag.Parse()

	if err := run(flag.Arg(0), *fixtureDir, *preserve); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(srcChart, fixturesDir string, preserve bool) error {
	if srcChart == "" {
		return fmt.Errorf("chart directory is required")
	}

	dir, err := os.MkdirTemp("", "")
	if err != nil {
		return err
	}
	defer func() {
		if preserve {
			fmt.Fprintf(os.Stdout, "Preserving temporary directory: %s\n", dir)
			return
		}

		err = os.RemoveAll(dir)
	}()

	chartDir := filepath.Join(dir, "chart")
	resultsDir := filepath.Join(dir, "results")

	err = exec.Command("cp", "-r", srcChart, chartDir).Run()
	if err != nil {
		return fmt.Errorf("copying chart: %w", err)
	}

	comments, err := injectComments(chartDir)
	if err != nil {
		return fmt.Errorf("injecting comments: %w", err)
	}

	finalFixturesDir := fixturesDir
	if !filepath.IsAbs(finalFixturesDir) {
		finalFixturesDir = filepath.Join(srcChart, fixturesDir)
	}
	finalFixturesDir, _ = filepath.Abs(finalFixturesDir)
	fixtures, err := os.ReadDir(finalFixturesDir)
	if err != nil {
		return fmt.Errorf("reading fixtures directory: %w", err)
	}

	var grp errgroup.Group
	grp.SetLimit(runtime.NumCPU() * 2)
	outputDirs := []string{}
	for _, fixture := range fixtures {
		if fixture.IsDir() || !strings.HasSuffix(fixture.Name(), ".yaml") {
			continue
		}

		fixturePath := filepath.Join(finalFixturesDir, fixture.Name())
		dir := filepath.Join(resultsDir, strings.TrimSuffix(fixture.Name(), ".yaml"))
		outputDirs = append(outputDirs, dir)

		grp.Go(func() error {
			out, err := exec.Command("helm", "template", "--output-dir", dir, "--values", fixturePath, chartDir).CombinedOutput()
			if err != nil {
				return fmt.Errorf("rendering chart: %s", string(out))
			}
			return nil
		})
	}
	if err := grp.Wait(); err != nil {
		return err
	}

	ids, err := discoverComments(resultsDir)
	if err != nil {
		return fmt.Errorf("discovering comments: %w", err)
	}

	var failed bool
	for id, def := range comments {
		if slices.Contains(ids, id) {
			continue
		}
		fmt.Fprintf(os.Stdout, "FAIL:\n  Branch was not found in the rendered chart output.\n  %s\n\n\n", def)
		failed = true
	}

	for _, dir := range outputDirs {
		out, err := exec.Command("conftest", "test", "--policy", filepath.Join(chartDir, "policy"), dir).CombinedOutput()
		if err != nil {
			failed = true
		}
		fmt.Fprintf(os.Stdout, "Conftest output (%s):\n%s\n\n", filepath.Base(dir), string(out))
	}

	if failed {
		return fmt.Errorf("test failure")
	}
	return nil
}

var conditionalRegex = regexp.MustCompile(`{{-?\s*if`)

func injectComments(dir string) (map[string]string, error) {
	comments := make(map[string]string)
	return comments, filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".yaml") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		lines := strings.Split(string(content), "\n")
		for i, line := range lines {
			if !conditionalRegex.MatchString(line) {
				continue
			}
			id := uuid.NewString()
			comments[id] = strings.TrimSpace(line)
			lines[i] = fmt.Sprintf("%s\n # helmlint: %s", line, id)
		}

		return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
	})
}

func discoverComments(dir string) ([]string, error) {
	ids := []string{}
	return ids, filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".yaml") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		for _, line := range strings.Split(string(content), "\n") {
			const prefix = "# helmlint: "
			if strings.Contains(line, prefix) {
				ids = append(ids, strings.TrimPrefix(strings.TrimSpace(line), prefix))
			}
		}

		return nil
	})
}
