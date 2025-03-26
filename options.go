package helmlint

import (
	"os"
	"path/filepath"
	"runtime"
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
func WithFixturesDir(dir string) Option {
	return func(o *options) {
		o.FixturesDir = dir
	}
}

// WithPolicyDir overrides the default policies directory, which is "policies" in the chart directory.
func WithPoliciesDir(dir string) Option {
	return func(o *options) {
		o.PoliciesDir = dir
	}
}

type options struct {
	Preserve        bool
	WriteExceptions bool
	Concurrency     int
	ChartDir        string
	FixturesDir     string
	PoliciesDir     string
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

	o.FixturesDir, err = o.chartRelPath(o.FixturesDir, "fixtures")
	if err != nil {
		return err
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
