package commands

import "testing"

func TestGetDate(t *testing.T) {
	_, err := GetDate("aaaa")
	if err == nil {
		t.Errorf("Expected error, got nil")
	}

	_, err = GetDate("1488452400 +0100")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// @epoch format used by git commit --amend in hook context
	_, err = GetDate("@1488452400 +0100")
	if err != nil {
		t.Errorf("Expected no error for @epoch format, got %v", err)
	}

	t.Setenv("GIT_AUTHOR_DATE", "2005-04-07 22:13:13")
	_, err = GetDate("")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	t.Setenv("GIT_COMMITTER_DATE", "Thu, 07 Apr 2005 22:13:13 +0200")
	t.Setenv("GIT_AUTHOR_DATE", "")
	_, err = GetDate("")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}
