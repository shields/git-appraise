package commands

import (
	"errors"
	"flag"
	"fmt"
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

func webGenerateStatic(repoDetails *web.RepoDetails) error {
	var paths web.StaticPaths

	if err := repoDetails.Update(); err != nil {
		return err
	}
	if err := os.Mkdir(*outputDir, os.ModeDir | 0755); err != nil {
		if errors.Is(err, os.ErrExist) {
			// Nothing to do
		} else {
			return err
		}
	}
	if err := os.Chdir(*outputDir); err != nil {
		return err
	}

	cssFile, err := os.Create(paths.Css())
	if err != nil {
		return err
	}
	if err := web.WriteStyleSheet(cssFile); err != nil {
		return err
	}

	repoFile, err := os.Create(paths.Repo())
	if err != nil {
		return err
	}
	if err := repoDetails.WriteRepoTemplate(paths, repoFile); err != nil {
		return err
	}

	for idx, branch := range repoDetails.Branches {
		idx := uint64(idx)
		branchFile, err := os.Create(paths.Branch(idx))
		if err != nil {
			return err
		}
		if err := repoDetails.WriteBranchTemplate(idx, paths, branchFile); err != nil {
			return err
		}

		for _, reviews := range [][]review.Summary{branch.OpenReviews, branch.ClosedReviews} {
			for _, review := range reviews {
				reviewFile, err := os.Create(paths.Review(review.Revision))
				if err != nil {
					return err
				}
				if err := repoDetails.WriteReviewTemplate(review.Revision, paths, reviewFile); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func webServe(repoDetails *web.RepoDetails) error {
	http.HandleFunc("/_ah/health",
		func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "ok")
		})

	var paths web.ServePaths

	stylesheet, _, _ := strings.Cut(paths.Css(), "?")
	repo, _, _       := strings.Cut(paths.Repo(), "?")
	branch, _, _     := strings.Cut(paths.Branch(0), "?")
	review, _, _     := strings.Cut(paths.Review(""), "?")

	http.HandleFunc("/" + stylesheet, web.ServeStyleSheet)
	http.HandleFunc("/" + repo, repoDetails.ServeRepoTemplate)
	http.HandleFunc("/" + branch, repoDetails.ServeBranchTemplate)
	http.HandleFunc("/" + review, repoDetails.ServeReviewTemplate)
	http.HandleFunc("/", repoDetails.ServeEntryPointRedirect)

	return http.ListenAndServe(fmt.Sprintf(":%d", *port), nil)
}

func usage(arg0 string) {
	fmt.Printf("Usage: %s web [-port <num> | -output <dir>]\n\nOptions:\n", arg0)
	webFlagSet.PrintDefaults()
}

var webCmd = &Command{
	Usage: usage,
	RunMethod: func(repo repository.Repo, args []string) error {
		webFlagSet.Parse(args)
		args = webFlagSet.Args()
		repoDetails, err := web.NewRepoDetails(repo)
		if err != nil {
			return err
		}
		if *outputDir != "" {

			if err := webGenerateStatic(repoDetails); err != nil {
				return err
			}
		}
		if *port != 0 {
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
