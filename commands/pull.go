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

package commands

import (
	"errors"
	"flag"
	"fmt"

	"msrl.dev/git-appraise/repository"
)

var pullFlagSet = flag.NewFlagSet("pull", flag.ExitOnError)

// pull updates the local git-notes used for reviews with those from a remote
// repo.
func pull(repo repository.Repo, args []string) error {
	pullFlagSet.Parse(args)
	pullArgs := pullFlagSet.Args()

	if len(pullArgs) > 1 {
		return errors.New(
			"Only pulling from one remote at a time is supported.")
	}

	remote := "origin"
	if len(pullArgs) == 1 {
		remote = pullArgs[0]
	}
	return repo.PullNotesAndArchive(remote, notesRefPattern, archiveRefPattern)
}

var pullCmd = &Command{
	Usage: func(arg0 string) {
		fmt.Printf("Usage: %s pull [<option>] [<remote>]\n\nOptions:\n", arg0)
		pullFlagSet.PrintDefaults()
	},
	RunMethod: func(repo repository.Repo, args []string) error {
		return pull(repo, args)
	},
}
