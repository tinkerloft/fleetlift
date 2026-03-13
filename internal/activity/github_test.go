package activity

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractOwnerRepo(t *testing.T) {
	cases := []struct {
		name  string
		url   string
		owner string
		repo  string
	}{
		{
			name:  "plain https URL",
			url:   "https://github.com/org/repo",
			owner: "org",
			repo:  "repo",
		},
		{
			name:  "https URL with .git suffix",
			url:   "https://github.com/org/repo.git",
			owner: "org",
			repo:  "repo",
		},
		{
			name:  "https URL with trailing slash",
			url:   "https://github.com/org/repo/",
			owner: "org",
			repo:  "repo",
		},
		{
			name:  "https URL with .git and trailing slash",
			url:   "https://github.com/org/repo.git/",
			owner: "org",
			repo:  "repo",
		},
		{
			// Single path segment: extractOwnerRepo returns the two rightmost slash-delimited
			// tokens, so for "https://github.com/short" the owner is "github.com" and repo
			// is "short". The empty-string sentinel is only returned when len(parts) < 2
			// (e.g., an empty input string).
			name:  "single segment URL uses last two path components",
			url:   "https://github.com/short",
			owner: "github.com",
			repo:  "short",
		},
		{
			name:  "empty string returns empty owner and repo",
			url:   "",
			owner: "",
			repo:  "",
		},
		{
			name:  "nested org URL",
			url:   "https://github.com/myorg/myrepo.git",
			owner: "myorg",
			repo:  "myrepo",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o, r := extractOwnerRepo(c.url)
			assert.Equal(t, c.owner, o, "url=%s owner", c.url)
			assert.Equal(t, c.repo, r, "url=%s repo", c.url)
		})
	}
}
