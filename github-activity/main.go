package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Event represents a single GitHub event in the returned JSON array.

type Event struct {
	Type    string          `json:"type"`
	Repo    Repo            `json:"repo"`
	Payload json.RawMessage `json:"payload"`
}

type Repo struct {
	Name string `json:"name"` // JSON "name" is like "owner/repo".
}

type PushPayload struct {
	Commits []struct {
		SHA string `json:"sha"`
	} `json:"commits"` // JSON array "commits".
}

type ActionPayload struct {
	Action string `json:"action"`
}

type CreatePayload struct {
	RefType string `json:"ref_type"` // "repository", "branch", "tag".
	Ref     string `json:"ref"`      // Name of branch/tag created (empty for repo sometimes).
}

// PullRequestPayload matches the payload structure for PullRequestEvent.
type PullRequestPayload struct {
	Action      string `json:"action"` // "opened", "closed", "reopened", etc.
	PullRequest struct {
		Merged bool `json:"merged"` // True if it was merged.
	} `json:"pull_request"` // Nested object "pull_request".
}

/*************************************************************/

func usage() {
	fmt.Fprintln(os.Stderr, "Use it this whay : github-activity <username>")
}

func main() {
	if len(os.Args) != 2 {
		// not enough args
		usage()
		os.Exit(1)
	}

	username := os.Args[1]

	//calling the function to fetch github events
	events, err := fetchEvents(username)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error", err)
		os.Exit(1)
	}

	//no recent activity found
	if len(events) == 0 {
		fmt.Println("no recent activity")
		return
	}

	for _, e := range events {
		line := formatEvent(e)
		if line != "" {
			fmt.Println("-", line)
		}
	}

}

func fetchEvents(username string) ([]Event, error) {
	url := fmt.Sprintf("https://api.github.com/users/%s/events", username)

	//make http client
	client := &http.Client{Timeout: 10 * time.Second}

	//create a get request
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	//set user agent for header
	req.Header.Set("User-Agent", "github-activity-cli")

	req.Header.Set("Accept", "application/vnd.github+json")

	//send the request
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed %w", err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response %w", err)
	}

	//handle status code
	switch res.StatusCode {
	case 200:
		//ok
	case 404:
		//unknown user
		return nil, fmt.Errorf("user %s not found", username)
	case 403:
		//forbidden/else
		return nil, fmt.Errorf("forbidden 403, response %s", string(body))
	default:
		return nil, fmt.Errorf("github api error %s resp: %s", res.Status, string(body))
	}

	var events []Event
	if err := json.Unmarshal(body, &events); err != nil {
		return nil, fmt.Errorf("invalid json %w", err)
	}
	return events, nil

}

// formatEvent converts an even into human readable line
func formatEvent(e Event) string {
	repo := e.Repo.Name
	switch e.Type {

	case "PushEvent":
		// For PushEvent, payload contains "commits": [...]
		var p PushPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			// If payload decode fails, still print something generic.
			return fmt.Sprintf("Pushed commits to %s", repo)
		}

		n := len(p.Commits) // Count commits in the push.

		// Singular vs plural grammar.
		if n == 1 {
			return fmt.Sprintf("Pushed 1 commit to %s", repo)
		}
		return fmt.Sprintf("Pushed %d commits to %s", n, repo)

	case "IssuesEvent":
		// IssuesEvent payload includes an "action" like "opened".
		action := extractAction(e.Payload) // Try to parse action.
		if action == "" {                  // If missing, fallback
			action = "updated"
		}
		// verbCap just capitalizes the first letter: "opened" -> "Opened"
		return fmt.Sprintf("%s an issue in %s", verbCap(action), repo)

	case "IssueCommentEvent":
		// Comment on an issue.
		return fmt.Sprintf("Commented on an issue in %s", repo)

	case "PullRequestEvent":
		// PullRequestEvent payload includes action and merged flag.
		var p PullRequestPayload
		if err := json.Unmarshal(e.Payload, &p); err == nil { // If decode worked
			// If closed + merged=true, we want to say "Merged" not "Closed".
			if p.Action == "closed" && p.PullRequest.Merged {
				return fmt.Sprintf("Merged a pull request in %s", repo)
			}
			// Otherwise print the action if present.
			if p.Action != "" {
				return fmt.Sprintf("%s a pull request in %s", verbCap(p.Action), repo)
			}
		}
		// If decode failed or action missing, fallback message.
		return fmt.Sprintf("Updated a pull request in %s", repo)

	case "WatchEvent":
		// GitHub uses WatchEvent with action "started" for stars.
		return fmt.Sprintf("Starred %s", repo)

	case "ForkEvent":
		// Someone forked a repo.
		return fmt.Sprintf("Forked %s", repo)

	case "CreateEvent":
		// CreateEvent payload can say what was created (branch/tag/repo).
		var p CreatePayload
		if err := json.Unmarshal(e.Payload, &p); err == nil { // If decode worked
			if p.RefType == "repository" { // Creating repo
				return fmt.Sprintf("Created repository %s", repo)
			}
			if p.Ref != "" { // Creating branch/tag with a name
				return fmt.Sprintf("Created %s %q in %s", p.RefType, p.Ref, repo)
			}
			// Creating branch/tag without a ref name (rare)
			return fmt.Sprintf("Created %s in %s", p.RefType, repo)
		}
		// If decode failed, fallback.
		return fmt.Sprintf("Created something in %s", repo)

	default:
		// For any event types we didn't explicitly handle:
		if repo != "" { // If repo is known, include it
			return fmt.Sprintf("%s in %s", e.Type, repo)
		}
		// Otherwise just print event type.
		return e.Type
	}
}

// extractAction tries to decode the payload as ActionPayload and return payload.action.
func extractAction(payload json.RawMessage) string {
	var p ActionPayload                                 // Create a variable to decode into.
	if err := json.Unmarshal(payload, &p); err != nil { // Decode JSON into p.
		return "" // If decoding fails, no action.
	}
	return p.Action // Return the action string.
}

// verbCap capitalizes the first letter of a string ("opened" -> "Opened").
func verbCap(action string) string {
	if action == "" { // If empty, return empty.
		return ""
	}

	b := []byte(action) // Convert string to bytes so we can edit the first character.

	// If first character is lowercase a-z, convert it to uppercase.
	if b[0] >= 'a' && b[0] <= 'z' {
		b[0] = b[0] - ('a' - 'A')
	}

	return string(b) // Convert back to string and return.
}
