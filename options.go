package helmlint

import (
	"path/filepath"
	"runtime"
)

type Option func(*options)

// WithMaxConcurrency sets the maximum number of concurrent goroutines used by the linters.
// Defaults to 2 * runtime.NumCPU().
func WithMaxConcurrency(n int) Option {
	return func(o *options) {
		o.Concurrency = n
	}
}

// WithPreserve causes the temporary directory to be logged after the test instead of being deleted.
// Useful for debugging.
func WithPreserve() Option {
	return func(o *options) {
		o.Preserve = true
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
	Preserve    bool
	Concurrency int
	ChartDir    string
	FixturesDir string
	PoliciesDir string
}

func (o *options) Finalize() (err error) {
	if o.Concurrency == 0 {
		o.Concurrency = runtime.NumCPU() * 2
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
