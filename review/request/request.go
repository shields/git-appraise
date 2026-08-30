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

// Package request defines the internal representation of a review request.
package request

import (
	jsonv1 "encoding/json"
	jsonv2 "encoding/json/v2"
	"strconv"
	"time"

	"msrl.dev/git-appraise/repository"
)

// Ref defines the git-notes ref that we expect to contain review requests.
const Ref = "refs/notes/devtools/reviews"

// FormatVersion defines the latest version of the request format supported by the tool.
const FormatVersion = 0

// Request represents an initial request for a code review.
//
// Timestamp and Requester are required (see schema/request.json); the other
// fields are optional.
type Request struct {
	// Timestamp and Requester are optimizations that allows us to display reviews
	// without having to run git-blame over the notes object. This is done because
	// git-blame will become more and more expensive as the number of reviews grows.
	Timestamp   string   `json:"timestamp"`
	ReviewRef   string   `json:"reviewRef,omitempty"`
	TargetRef   string   `json:"targetRef"`
	Requester   string   `json:"requester"`
	Reviewers   []string `json:"reviewers,omitempty"`
	Description string   `json:"description,omitempty"`
	// Version represents the version of the metadata format.
	Version int `json:"v,omitzero"`
	// BaseCommit stores the commit ID of the target ref at the time the review was requested.
	// This is optional, and only used for submitted reviews which were anchored at a merge commit.
	// This allows someone viewing that submitted review to find the diff against which the
	// code was reviewed.
	BaseCommit string `json:"baseCommit,omitempty"`
	// Alias stores a post-rebase commit ID for the review. This allows the tool
	// to track the history of a review even if the commit history changes.
	Alias string `json:"alias,omitempty"`
}

// New returns a new request.
//
// The Timestamp is set to the current time so the request satisfies the schema
// even when the caller does not set one; callers that honor an explicit commit
// date may overwrite it. The Requester is the given requester.
func New(requester string, reviewers []string, reviewRef, targetRef, description string) Request {
	return Request{
		Timestamp:   strconv.FormatInt(time.Now().Unix(), 10),
		Requester:   requester,
		Reviewers:   reviewers,
		ReviewRef:   reviewRef,
		TargetRef:   targetRef,
		Description: description,
	}
}

// Parse parses a review request from a git note.
func Parse(note repository.Note) (Request, error) {
	bytes := []byte(note)
	var request Request
	err := jsonv2.Unmarshal(bytes, &request, jsonv1.DefaultOptionsV1())
	// TODO(ojarjur): If "requester" is not set, then use git-blame to fill it in.
	return request, err
}

// ParseAllValid takes collection of git notes and tries to parse a review
// request from each one. Any notes that are not valid review requests get
// ignored, as we expect the git notes to be a heterogenous list, with only
// some of them being review requests.
func ParseAllValid(notes []repository.Note) []Request {
	var requests []Request
	for _, note := range notes {
		request, err := Parse(note)
		if err == nil && request.Version == FormatVersion {
			requests = append(requests, request)
		}
	}
	return requests
}

// Write writes a review request as a JSON-formatted git note.
func (request *Request) Write() (repository.Note, error) {
	bytes, err := jsonv2.Marshal(request, jsonv1.DefaultOptionsV1())
	return repository.Note(bytes), err
}
