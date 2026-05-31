package commands

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"msrl.dev/git-appraise/commands/web"
	"msrl.dev/git-appraise/repository"
	"msrl.dev/git-appraise/review"
)

var webFlagSet = flag.NewFlagSet("web", flag.ExitOnError)

var (
	port      = webFlagSet.Uint("port", 0, "Web server port.")
	outputDir = webFlagSet.String("output", "", "Static HTML output directory.")
)

// createFileFn is overridable in tests. The static-html error branches
// require an open file whose subsequent Write fails — historically tested
// with a symlink to Linux's /dev/full, which does not exist on Darwin or
// Windows. Injection makes the WriteStyleSheet/RepoTemplate/etc. error
// paths reachable on every platform.
var createFileFn = os.Create

func webGenerateStatic(repoDetails *web.RepoDetails) error {
	var paths web.StaticPaths

	if err := repoDetails.Update(); err != nil {
		return err
	}
	if err := os.Mkdir(*outputDir, 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			// Nothing to do
		} else {
			return err
		}
	}
	origCwd, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := os.Chdir(*outputDir); err != nil {
		return err
	}
	defer os.Chdir(origCwd)

	if err := writeFile(paths.Css(), web.WriteStyleSheet); err != nil {
		return err
	}
	if err := writeFile(paths.Repo(), func(w io.Writer) error {
		return repoDetails.WriteRepoTemplate(paths, w)
	}); err != nil {
		return err
	}

	for idx, branch := range repoDetails.Branches {
		idx := uint64(idx)
		if err := writeFile(paths.Branch(idx), func(w io.Writer) error {
			return repoDetails.WriteBranchTemplate(idx, paths, w)
		}); err != nil {
			return err
		}

		for _, reviews := range [][]review.Summary{branch.OpenReviews, branch.ClosedReviews} {
			for _, review := range reviews {
				if err := writeFile(paths.Review(review.Revision), func(w io.Writer) error {
					return repoDetails.WriteReviewTemplate(review.Revision, paths, w)
				}); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// writeFile opens path via createFileFn, hands the writer to write, and
// reports the first error among write or close. Surfacing Close() errors
// is necessary because buffered data may fail to flush (e.g., ENOSPC).
func writeFile(path string, write func(io.Writer) error) (err error) {
	f, err := createFileFn(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	return write(f)
}

func webSetupHandlers(repoDetails *web.RepoDetails) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/_ah/health",
		func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "ok")
		})

	var paths web.ServePaths

	stylesheet, _, _ := strings.Cut(paths.Css(), "?")
	repo, _, _ := strings.Cut(paths.Repo(), "?")
	branch, _, _ := strings.Cut(paths.Branch(0), "?")
	review, _, _ := strings.Cut(paths.Review(""), "?")

	mux.HandleFunc("/"+stylesheet, web.ServeStyleSheet)
	mux.HandleFunc("/"+repo, repoDetails.ServeRepoTemplate)
	mux.HandleFunc("/"+branch, repoDetails.ServeBranchTemplate)
	mux.HandleFunc("/"+review, repoDetails.ServeReviewTemplate)
	mux.HandleFunc("/", repoDetails.ServeEntryPointRedirect)

	return mux
}

func webServe(repoDetails *web.RepoDetails) error {
	mux := webSetupHandlers(repoDetails)
	return http.ListenAndServe(fmt.Sprintf(":%d", *port), mux)
}

func usage(arg0 string) {
	fmt.Printf("Usage: %s web [-port <num> | -output <dir>]\n\nOptions:\n", arg0)
	webFlagSet.PrintDefaults()
}

var webCmd = &Command{
	Usage: usage,
	RunMethod: func(repo repository.Repo, args []string) error {
		webFlagSet.Parse(args)
		repoDetails := web.NewRepoDetails(repo)
		if *outputDir != "" {
			if err := webGenerateStatic(repoDetails); err != nil {
				return err
			}
		}
		if *port != 0 {
			if err := repoDetails.Update(); err != nil {
				return err
			}
			if err := webServe(repoDetails); err != nil {
				return err
			}
		}
		if *outputDir == "" && *port == 0 {
			usage(os.Args[0])
			fmt.Println()
			return errors.New("Expected one of -port or -output")
		}
		return nil
	},
}
