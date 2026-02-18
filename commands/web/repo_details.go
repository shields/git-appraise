package web

import (
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"msrl.dev/git-appraise/repository"
	"msrl.dev/git-appraise/review"
)

type ReviewType int

const (
	OpenReview ReviewType = iota
	ClosedReview
	AbandonedReview
)

type ReviewIndex struct {
	Type ReviewType
	// Index into RepoDetails.Branches[...] for Open/ClosedReview
	Branch int
	// Index into RepoDetails.{Branches[...].{OpenReviews,ClosedReviews},.AbandonedReviews}
	Index int
}

type BranchDetails struct {
	Ref               string
	Title             string
	Subtitle          string
	Description       string
	OpenReviewCount   int
	OpenReviews       []review.Summary
	ClosedReviewCount int
	ClosedReviews     []review.Summary
}

type BranchList []*BranchDetails

func (list BranchList) Len() int           { return len(list) }
func (list BranchList) Swap(i, j int)      { list[i], list[j] = list[j], list[i] }
func (list BranchList) Less(i, j int) bool { return list[i].Ref < list[j].Ref }

type RepoDetails struct {
	Path             string
	Repo             repository.Repo
	RepoHash         string
	Title            string
	Subtitle         string
	Description      string
	Branches         BranchList
	AbandonedReviews []review.Summary
	ReviewMap        map[string]ReviewIndex
}

func (reviewIndex *ReviewIndex) GetBranchTitle(repoDetails *RepoDetails) string {
	if reviewIndex.Type == OpenReview || reviewIndex.Type == ClosedReview {
		return repoDetails.Branches[reviewIndex.Branch].Title
	} else {
		return ""
	}
}

func (reviewIndex *ReviewIndex) GetSummaries(repoDetails *RepoDetails) []review.Summary {
	switch reviewIndex.Type {
	case OpenReview:
		return repoDetails.Branches[reviewIndex.Branch].OpenReviews
	case ClosedReview:
		return repoDetails.Branches[reviewIndex.Branch].ClosedReviews
	case AbandonedReview:
		return repoDetails.AbandonedReviews
	}
	return nil
}

func (reviewIndex *ReviewIndex) GetSummary(repoDetails *RepoDetails) *review.Summary {
	summaries := reviewIndex.GetSummaries(repoDetails)
	if reviewIndex.Index < len(summaries) {
		return &summaries[reviewIndex.Index]
	} else {
		return nil
	}
}

func (reviewIndex *ReviewIndex) GetPrevious(repoDetails *RepoDetails) *ReviewIndex {
	if reviewIndex.Index > 0 {
		previousIndex := *reviewIndex
		previousIndex.Index -= 1
		return &previousIndex
	} else if reviewIndex.Branch > 0 &&
		(reviewIndex.Type == OpenReview || reviewIndex.Type == ClosedReview) {
		previousIndex := *reviewIndex
		previousIndex.Branch -= 1
		previousIndex.Index = len(previousIndex.GetSummaries(repoDetails)) - 1
		return &previousIndex
	}
	return nil
}

func (reviewIndex *ReviewIndex) GetNext(repoDetails *RepoDetails) *ReviewIndex {
	if reviewIndex.Index < len(reviewIndex.GetSummaries(repoDetails))-1 {
		nextIndex := *reviewIndex
		nextIndex.Index += 1
		return &nextIndex
	} else if reviewIndex.Branch < len(repoDetails.Branches)-1 &&
		(reviewIndex.Type == OpenReview || reviewIndex.Type == ClosedReview) {
		nextIndex := *reviewIndex
		nextIndex.Branch += 1
		nextIndex.Index = 0
		return &nextIndex
	}
	return nil
}

var repoDescriptionRe = regexp.MustCompile(`(# (.*)\n)?(## (.*)\n)?((?s).*)`)

const descriptionPath = "README.md"

// Parses the repo description format, a markdown with optional `# Title` and
// `## Subtitle` at the start of the file.
func ParseDescription(text string) (title, subtitle, description string) {
	split := repoDescriptionRe.FindStringSubmatch(text)
	if split[2] != "" {
		title = split[2]
	}
	if split[4] != "" {
		subtitle = split[4]
	}
	description = split[5]
	return
}

func (repoDetails *RepoDetails) UpdateRepoDescription() {

	description, err := repoDetails.Repo.Show("HEAD", descriptionPath)
	if err == nil {
		repoDetails.Title, repoDetails.Subtitle, repoDetails.Description = ParseDescription(string(description))
	}
	if repoDetails.Title == "" {
		repoPath := repoDetails.Repo.GetPath()
		repoDetails.Title = path.Base(repoPath)
	}
}

// NewRepoDetails constructs a RepoDetails instance from the given Repo instance.
func NewRepoDetails(repo repository.Repo) (*RepoDetails, error) {
	repoDetails := &RepoDetails{Path: repo.GetPath(), Repo: repo}
	repoDetails.UpdateRepoDescription()
	return repoDetails, nil
}

// GetBranchDetails constructs a concise summary of the branch.
func (repoDetails *RepoDetails) GetBranchDetails(branch string) *BranchDetails {
	details := &BranchDetails{Ref: branch}
	description, err := repoDetails.Repo.Show(branch, descriptionPath)
	if err == nil {
		details.Title, details.Subtitle, details.Description = ParseDescription(description)
	}
	if details.Title == "" {
		details.Title = strings.TrimPrefix(branch, "refs/heads/")
	}
	return details
}

func (repoDetails *RepoDetails) Update() error {
	stateHash, err := repoDetails.Repo.GetRepoStateHash()
	if err != nil {
		return err
	}
	if stateHash == repoDetails.RepoHash {
		return nil
	}

	repoDetails.UpdateRepoDescription()

	branchesSet := make(map[string]*BranchDetails)
	allReviews := review.ListAll(repoDetails.Repo)
	openReviews := make(map[string][]review.Summary)
	closedReviews := make(map[string][]review.Summary)
	var abandonedReviews []review.Summary
	for _, review := range allReviews {
		if review.Request.TargetRef == "" {
			abandonedReviews = append(abandonedReviews, review)
		} else {
			branch := review.Request.TargetRef
			if branchesSet[branch] == nil {
				branchesSet[branch] = repoDetails.GetBranchDetails(branch)
			}
			if review.Submitted {
				closedReviews[branch] = append(closedReviews[branch], review)
			} else {
				openReviews[branch] = append(openReviews[branch], review)
			}
		}
	}

	// We want oldest to newest, for our book/narrative purposes
	slices.Reverse(abandonedReviews)
	var branches BranchList
	for _, branch := range branchesSet {
		slices.Reverse(openReviews[branch.Ref])
		slices.Reverse(closedReviews[branch.Ref])

		branch.OpenReviewCount = len(openReviews[branch.Ref])
		branch.OpenReviews = openReviews[branch.Ref]
		branch.ClosedReviewCount = len(closedReviews[branch.Ref])
		branch.ClosedReviews = closedReviews[branch.Ref]

		branches = append(branches, branch)
	}
	sort.Stable(branches)

	reviewMap := make(map[string]ReviewIndex)
	for index, abandoned := range abandonedReviews {
		reviewMap[abandoned.Revision] = ReviewIndex{
			Type:  AbandonedReview,
			Index: index,
		}
	}
	for branchIndex, branch := range branches {
		for reviewIndex, review := range branch.OpenReviews {
			reviewMap[review.Revision] = ReviewIndex{
				Type:   OpenReview,
				Branch: branchIndex,
				Index:  reviewIndex,
			}
		}
		for reviewIndex, review := range branch.ClosedReviews {
			reviewMap[review.Revision] = ReviewIndex{
				Type:   ClosedReview,
				Branch: branchIndex,
				Index:  reviewIndex,
			}
		}
	}

	repoDetails.Branches = branches
	repoDetails.AbandonedReviews = abandonedReviews
	repoDetails.RepoHash = stateHash
	repoDetails.ReviewMap = reviewMap

	return nil
}
