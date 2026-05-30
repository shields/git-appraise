package main

import (
	"bytes"
	_ "embed"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"msrl.dev/git-appraise/commands/web"
	"msrl.dev/git-appraise/repository"
)

var port = flag.Uint("port", 0, "Web server port.")

//go:embed repos.html
var repos_html string

type ServeMultiPaths struct{}

func (ServeMultiPaths) Css() string  { return "/stylesheet.css" }
func (ServeMultiPaths) Repo() string { return "repo.html" }
func (ServeMultiPaths) Branch(branch uint64) string {
	return fmt.Sprintf("branch.html?branch=%d", branch)
}
func (ServeMultiPaths) Review(review string) string {
	return fmt.Sprintf("review.html?review=%s", review)
}

type reposMap map[string]*web.RepoDetails
type Repos atomic.Pointer[reposMap]

// getwdFn is overridable in tests; on Darwin os.Getwd returns the cached
// path even after the cwd is removed, so the only portable way to exercise
// the error branch is via injection.
var getwdFn = os.Getwd

func (oldRepos *Repos) Discover() error {
	var newRepos = make(reposMap)

	cwd, err := getwdFn()
	if err != nil {
		return err
	}

	err = filepath.Walk(cwd, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// filepath.Rel cannot fail here: path comes from Walk(cwd, ...)
			// so it is always relative to cwd.
			relPath, _ := filepath.Rel(cwd, path)
			gitRepo, err := repository.NewGitRepo(relPath)
			if err != nil {
				return nil
			}
			repoDetails := web.NewRepoDetails(gitRepo)
			if err := repoDetails.Update(); err != nil {
				// This directory is a git repo, just an unusable one; never
				// descend into it (including its .git directory).
				return filepath.SkipDir
			}
			// Key the map and the URL-facing Path by the same cwd-relative
			// name, normalized to forward slashes so the rendered URLs and
			// the /{repo}/... routes match on every platform. NewRepoDetails
			// derives Path from the repo's absolute filesystem root, which
			// (a) leaks the server's directory layout in the rendered links
			// and (b) never matches this map key, so the repos.html links
			// would not route back to the entry.
			//
			// The routes registered in webServe use a single {repo} path
			// segment, so the key must itself be a single segment. When cwd is
			// a repo, relPath is "." (which HTTP clients normalize away); use
			// the directory's base name instead so the entry stays reachable.
			// Repos nested more than one level below cwd (relPath containing a
			// slash) are intentionally left keyed by their relative path: the
			// suffix-based /{repo}/repo.html scheme cannot address them, and
			// inventing a flattened key risks colliding with a real top-level
			// repo, so multi-level layouts are an unsupported deployment.
			urlPath := filepath.ToSlash(relPath)
			if urlPath == "." {
				urlPath = filepath.Base(cwd)
			}
			// filepath.Base returns "/" at the filesystem root; strip any
			// leading slash so the rendered link is a relative "repo.html"
			// path rather than a protocol-relative "//repo.html" URL.
			urlPath = strings.TrimPrefix(urlPath, "/")
			repoDetails.Path = urlPath
			newRepos[urlPath] = repoDetails
			return filepath.SkipDir
		}
		return nil
	})

	if err != nil {
		return err
	}

	(*atomic.Pointer[reposMap])(oldRepos).Swap(&newRepos)
	return nil
}

func (ptr *Repos) Load() reposMap {
	return *(*atomic.Pointer[reposMap])(ptr).Load()
}
func (ptr *Repos) Store(value *reposMap) {
	(*atomic.Pointer[reposMap])(ptr).Store(value)
}

func (repos *Repos) ServeStyleSheet(w http.ResponseWriter, r *http.Request) {
	web.ServeStyleSheet(w, r)
}

func (repos *Repos) ServeRepoTemplate(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("repo")
	if repoDetails, found := repos.Load()[repo]; found {
		repoDetails.ServeRepoTemplateWith(ServeMultiPaths{}, w, r)
	} else {
		http.Error(w, "Repository "+repo+" not found!", http.StatusNotFound)
	}
}

func (repos *Repos) ServeBranchTemplate(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("repo")
	if repoDetails, found := repos.Load()[repo]; found {
		repoDetails.ServeBranchTemplateWith(ServeMultiPaths{}, w, r)
	} else {
		http.Error(w, "Repository "+repo+" not found!", http.StatusNotFound)
	}
}

func (repos *Repos) ServeReviewTemplate(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("repo")
	if repoDetails, found := repos.Load()[repo]; found {
		repoDetails.ServeReviewTemplateWith(ServeMultiPaths{}, w, r)
	} else {
		http.Error(w, "Repository "+repo+" not found!", http.StatusNotFound)
	}
}

func (repos *Repos) ServeReposTemplate(w http.ResponseWriter, r *http.Request) {
	type ReposInfo struct {
		Repos  reposMap
		GitWeb string
	}
	reposInfo := ReposInfo{
		Repos:  repos.Load(),
		GitWeb: "/gitweb",
	}
	var writer bytes.Buffer
	if err := web.ServeTemplate(reposInfo, ServeMultiPaths{}, &writer, "repos", repos_html); err != nil {
		web.ServeErrorTemplate(err, http.StatusInternalServerError, w)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(writer.Bytes())
}

func (repos *Repos) ServeEntryPointRedirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/repos.html", http.StatusTemporaryRedirect)
}

func webServe() {
	var paths ServeMultiPaths
	repos := Repos{}
	repos.Store(new(reposMap))

	repos.Discover()

	http.HandleFunc("/_ah/health",
		func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "ok")
		})

	setupReloadOnSignal(&repos)

	stylesheet, _, _ := strings.Cut(paths.Css(), "?")
	repo, _, _ := strings.Cut(paths.Repo(), "?")
	branch, _, _ := strings.Cut(paths.Branch(0), "?")
	review, _, _ := strings.Cut(paths.Review(""), "?")

	http.HandleFunc("/repos.html", repos.ServeReposTemplate)
	http.HandleFunc(stylesheet, repos.ServeStyleSheet)
	http.HandleFunc("/{repo}/"+repo, repos.ServeRepoTemplate)
	http.HandleFunc("/{repo}/"+branch, repos.ServeBranchTemplate)
	http.HandleFunc("/{repo}/"+review, repos.ServeReviewTemplate)
	http.HandleFunc("/", repos.ServeEntryPointRedirect)

	if err := http.ListenAndServe(fmt.Sprintf(":%d", *port), nil); err != nil {
		fmt.Printf("Error: %#v\n", err)
	}
}

func main() {
	flag.Parse()
	webServe()
}
