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

package ci

import (
	"msrl.dev/git-appraise/repository"
	"testing"
)

const testCINote1 = `{
	"Timestamp": "4",
	"URL": "www.google.com",
	"Status": "success"
}`

const testCINote2 = `{
	"Timestamp": "16",
	"URL": "www.google.com",
	"Status": "failure"
}`

const testCINote3 = `{
	"Timestamp": "30",
	"URL": "www.google.com",
	"Status": "something else"
}`

const testCINote4 = `{
	"Timestamp": "28",
	"URL": "www.google.com",
	"Status": "success"
}`

const testCINote5 = `{
	"Timestamp": "27",
	"URL": "www.google.com",
	"Status": "success"
}`

func TestCIReport(t *testing.T) {
	latestReport, err := GetLatestCIReport(ParseAllValid([]repository.Note{
		repository.Note(testCINote1),
		repository.Note(testCINote2),
	}))
	if err != nil {
		t.Fatal("Failed to properly fetch the latest report", err)
	}
	expected, err := Parse(repository.Note(testCINote2))
	if err != nil {
		t.Fatal("Failed to parse the expected report", err)
	}
	if *latestReport != expected {
		t.Fatal("This is not the latest ", latestReport)
	}
	latestReport, err = GetLatestCIReport(ParseAllValid([]repository.Note{
		repository.Note(testCINote1),
		repository.Note(testCINote2),
		repository.Note(testCINote3),
		repository.Note(testCINote4),
	}))
	if err != nil {
		t.Fatal("Failed to properly fetch the latest report", err)
	}
	expected, err = Parse(repository.Note(testCINote4))
	if err != nil {
		t.Fatal("Failed to parse the expected report", err)
	}
	if *latestReport != expected {
		t.Fatal("This is not the latest ", latestReport)
	}
}

func TestGetLatestCIReportEmpty(t *testing.T) {
	report, err := GetLatestCIReport(nil)
	if err != nil {
		t.Fatal(err)
	}
	if report != nil {
		t.Fatalf("expected nil for empty reports, got %+v", report)
	}

	report, err = GetLatestCIReport([]Report{})
	if err != nil {
		t.Fatal(err)
	}
	if report != nil {
		t.Fatalf("expected nil for empty slice, got %+v", report)
	}
}

func TestParseValid(t *testing.T) {
	note := repository.Note(`{"timestamp":"42","url":"http://example.com","status":"success"}`)
	report, err := Parse(note)
	if err != nil {
		t.Fatal(err)
	}
	if report.Timestamp != "42" || report.URL != "http://example.com" || report.Status != StatusSuccess {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestParseInvalid(t *testing.T) {
	_, err := Parse(repository.Note(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseAllValidFiltersInvalidStatus(t *testing.T) {
	notes := []repository.Note{
		repository.Note(testCINote1),
		repository.Note(testCINote3), // "something else" status - filtered out
		repository.Note(testCINote5),
	}
	reports := ParseAllValid(notes)
	if len(reports) != 2 {
		t.Fatalf("expected 2 valid reports, got %d", len(reports))
	}
}

func TestParseAllValidFiltersWrongVersion(t *testing.T) {
	notes := []repository.Note{
		repository.Note(`{"timestamp":"1","status":"success","v":1}`),
		repository.Note(testCINote1),
	}
	reports := ParseAllValid(notes)
	if len(reports) != 1 {
		t.Fatalf("expected 1 valid report, got %d", len(reports))
	}
}

func TestParseAllValidEmptyStatus(t *testing.T) {
	notes := []repository.Note{
		repository.Note(`{"timestamp":"1"}`),
	}
	reports := ParseAllValid(notes)
	if len(reports) != 1 {
		t.Fatalf("expected 1 report (empty status is valid), got %d", len(reports))
	}
}
