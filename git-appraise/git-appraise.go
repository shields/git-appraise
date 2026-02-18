/*
Copyright 2015 Google Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Command git-appraise manages code reviews stored as git-notes in the source repo.
//
// To install, run:
//
//	$ go install msrl.dev/git-appraise/git-appraise@latest
//
// And for usage information, run:
//
//	$ git-appraise help
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"msrl.dev/git-appraise/commands"
	"msrl.dev/git-appraise/repository"
)

const usageMessageTemplate = `Usage: %s <command>

Where <command> is one of:
  %s

For individual command usage, run:
  %s help <command>
`

func printUsage(w io.Writer, arg0 string) {
	var subcommands []string
	for subcommand := range commands.CommandMap {
		subcommands = append(subcommands, subcommand)
	}
	sort.Strings(subcommands)
	fmt.Fprintf(w, usageMessageTemplate, arg0, strings.Join(subcommands, "\n  "), arg0)
}

func printHelp(w io.Writer, args []string) {
	if len(args) < 3 {
		printUsage(w, args[0])
		return
	}
	subcommand, ok := commands.CommandMap[args[2]]
	if !ok {
		fmt.Fprintf(w, "Unknown command %q\n", args[2])
		printUsage(w, args[0])
		return
	}
	subcommand.Usage(args[0])
}

func run(w io.Writer, args []string, cwd string) error {
	repo, err := repository.NewGitRepo(cwd)
	if err != nil {
		return fmt.Errorf("%s must be run from within a git repo", args[0])
	}
	if len(args) < 2 {
		subcommand, ok := commands.CommandMap["list"]
		if !ok {
			return errors.New("unable to list reviews")
		}
		return subcommand.Run(repo, []string{})
	}
	subcommand, ok := commands.CommandMap[args[1]]
	if !ok {
		printUsage(w, args[0])
		return fmt.Errorf("unknown command: %q", args[1])
	}
	return subcommand.Run(repo, args[2:])
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "help" {
		printHelp(os.Stdout, os.Args)
		return
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Printf("Unable to get the current working directory: %q\n", err)
		return
	}
	if err := run(os.Stdout, os.Args, cwd); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
