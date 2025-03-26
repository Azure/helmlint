package helmlint

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
)

type Option func(*options)

// WithPreserve causes the temporary directory to be logged after the test instead of being deleted.
// Useful for debugging.
func WithPreserve() Option {
	return func(o *options) {
		o.Preserve = true
	}
}

// WithWriteExceptions causes the linter to update the chart to ignore conditional branches that
// are not currently covered by the given fixtures.
//
// Disabled by default. Can also be enabled by setting HELMLINT_WRITE_EXCEPTIONS=true.
func WithWriteExceptions() Option {
	return func(o *options) {
		o.WriteExceptions = true
	}
}

// WithMaxConcurrency sets the maximum number of concurrent goroutines used by the linters.
// Defaults to 2 * runtime.NumCPU().
func WithMaxConcurrency(n int) Option {
	return func(o *options) {
		o.Concurrency = n
	}
}

// WithChartDir sets the directory of the chart to be tested.
// Defaults to the current directory.
func WithChartDir(dir string) Option {
	return func(o *options) {
		o.ChartDir = dir
	}
}

// WithFixturesDir overrides the default fixtures directory, which is "fixtures" in the chart directory.
// Multiple fixtures directores are supported.
func WithFixturesDir(dir string) Option {
	return func(o *options) {
		o.FixturesDirs = append(o.FixturesDirs, dir)
	}
}

// WithPolicyDir overrides the default policies directory, which is "policies" in the chart directory.
func WithPoliciesDir(dir string) Option {
	return func(o *options) {
		o.PoliciesDir = dir
	}
}

// WithRecursion provides a hook for extracting a k8s manifest to be linted out of another resource.
// For example: if a chart renders some resources into a configmap, this hook can be used to lint the "nested" resources.
// Multiple recursions are allowed.
// Only the WithPolicyDir option is supported.
func WithRecursion(fn RecursionFn, opts ...Option) Option {
	return func(o *options) {
		ruleOpts := &options{ChartDir: o.ChartDir, PoliciesDir: o.PoliciesDir}
		for _, opt := range opts {
			opt(ruleOpts)
		}
		if err := ruleOpts.Finalize(); err != nil {
			panic(fmt.Errorf("finalizing rule configuration failed (very unlikely): %s", err))
		}

		o.Recursions = append(o.Recursions, &recursionRule{
			Fn:   fn,
			Opts: ruleOpts,
		})
	}
}

type RecursionFn func(renderedDir, outputDir string) error

type recursionRule struct {
	Fn   RecursionFn
	Opts *options
}

// RecurseConfigmap recurses into resource manifests stored in each key of a ConfigMap at the given file path relative to the chart output.
func RecurseConfigmap(manifestPath string) RecursionFn {
	return func(chartDir, outputDir string) error {
		confMap := struct {
			Data map[string]string
		}{}

		body, err := os.ReadFile(filepath.Join(chartDir, manifestPath))
		if err != nil {
			return err
		}

		if err := yaml.Unmarshal(body, &confMap); err != nil {
			return err
		}

		for key, value := range confMap.Data {
			err = os.WriteFile(filepath.Join(outputDir, key+".yaml"), []byte(value), 0644)
			if err != nil {
				return err
			}
		}
		return nil
	}
}

// WithVisitor is called for every fixture and given the path of the rendered chart directory.
// Multiple visitors are allowed.
// Might be called concurrently!
func WithVisitor(fn VisitorFn) Option {
	return func(o *options) {
		o.Visitors = append(o.Visitors, fn)
	}
}

type VisitorFn func(t T, dir string)

type options struct {
	Preserve        bool
	WriteExceptions bool
	Concurrency     int
	ChartDir        string
	FixturesDirs    []string
	PoliciesDir     string
	Recursions      []*recursionRule
	Visitors        []VisitorFn
}

func (o *options) Finalize() (err error) {
	if o.Concurrency == 0 {
		o.Concurrency = runtime.NumCPU() * 2
	}

	if !o.WriteExceptions {
		o.WriteExceptions = os.Getenv("HELMLINT_WRITE_EXCEPTIONS") == "true"
	}

	o.ChartDir, err = filepath.Abs(o.ChartDir)
	if err != nil {
		return err
	}

	if len(o.FixturesDirs) == 0 {
		o.FixturesDirs = []string{""}
	}
	for i, dir := range o.FixturesDirs {
		o.FixturesDirs[i], err = o.chartRelPath(dir, "fixtures")
		if err != nil {
			return err
		}
	}

	o.PoliciesDir, err = o.chartRelPath(o.PoliciesDir, "policies")
	return err
}

func (o *options) chartRelPath(path, defaultPath string) (p string, err error) {
	if path == "" {
		path = defaultPath
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(o.ChartDir, path)
	}
	return filepath.Abs(path)
}
