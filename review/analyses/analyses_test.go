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

package analyses

import (
	"fmt"
	"msrl.dev/git-appraise/repository"
	"net/http"
	"net/http/httptest"
	"testing"
)

const (
	mockOldReport = `{"timestamp": "0", "url": "https://this-url-does-not-exist.test/analysis.json"}`
	mockNewReport = `{"timestamp": "1", "url": "%s"}`
	mockResults   = `{
  "analyze_response": [{
    "note": [{
      "location": {
        "path": "file.txt",
        "range": {
          "start_line": 5
        }
      },
      "category": "test",
      "description": "This is a test"
    }]
  }]
}`
)

func mockHandler(t *testing.T) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		t.Log(r)
		fmt.Fprintln(w, mockResults)
	}
}

func TestGetLatestResult(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(mockHandler(t)))
	defer mockServer.Close()

	reports := ParseAllValid([]repository.Note{
		repository.Note([]byte(mockOldReport)),
		repository.Note(fmt.Appendf(nil, mockNewReport, mockServer.URL)),
	})

	report, err := GetLatestAnalysesReport(reports)
	if err != nil {
		t.Fatal("Unexpected error while parsing analysis reports", err)
	}
	if report == nil {
		t.Fatal("Unexpected nil report")
	}
	reportResult, err := report.GetLintReportResult()
	if err != nil {
		t.Fatal("Unexpected error while reading the latest report's results", err)
	}
	if len(reportResult) != 1 {
		t.Fatal("Unexpected report result", reportResult)
	}
}

func TestGetNotesFromServer(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(mockHandler(t)))
	defer mockServer.Close()

	report := Report{
		Timestamp: "1",
		URL:       mockServer.URL,
	}
	notes, err := report.GetNotes()
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 {
		t.Fatalf("expected 1 note, got %d", len(notes))
	}
	if notes[0].Category != "test" {
		t.Fatalf("unexpected category: %q", notes[0].Category)
	}
	if notes[0].Description != "This is a test" {
		t.Fatalf("unexpected description: %q", notes[0].Description)
	}
}

func TestGetLintReportResultEmptyURL(t *testing.T) {
	report := Report{Timestamp: "1", URL: ""}
	result, err := report.GetLintReportResult()
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatalf("expected nil result for empty URL, got %+v", result)
	}
}

func TestGetLatestAnalysesReportEmpty(t *testing.T) {
	report, err := GetLatestAnalysesReport(nil)
	if err != nil {
		t.Fatal(err)
	}
	if report != nil {
		t.Fatalf("expected nil for nil reports, got %+v", report)
	}

	report, err = GetLatestAnalysesReport([]Report{})
	if err != nil {
		t.Fatal(err)
	}
	if report != nil {
		t.Fatalf("expected nil for empty slice, got %+v", report)
	}
}

func TestParseValid(t *testing.T) {
	note := repository.Note(`{"timestamp":"42","url":"http://example.com","status":"lgtm"}`)
	report, err := Parse(note)
	if err != nil {
		t.Fatal(err)
	}
	if report.Timestamp != "42" || report.URL != "http://example.com" || report.Status != StatusLooksGoodToMe {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestParseInvalid(t *testing.T) {
	_, err := Parse(repository.Note(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseAllValidFiltersWrongVersion(t *testing.T) {
	notes := []repository.Note{
		repository.Note(`{"timestamp":"1","v":1,"status":"lgtm"}`),
		repository.Note(`{"timestamp":"2","status":"lgtm"}`),
	}
	reports := ParseAllValid(notes)
	if len(reports) != 1 {
		t.Fatalf("expected 1 valid report, got %d", len(reports))
	}
}
