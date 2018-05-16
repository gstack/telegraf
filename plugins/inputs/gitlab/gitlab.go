package gitlab

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/xanzy/go-gitlab"
)

const (
	description  = "Read metrics based on data exposed by the Gitlab API"
	sampleConfig = `
  ## This plugin reads information exposed by the Gitlab API (using /api/plugins.json endpoint).
  ##
  ## Endpoint:
  ## - only one URI is allowed
  endpoint = "https://gitlab.com"
  ## Token:
  ## - Personal access token in Gitlab (API scope required)
  token = "abcd1234"
  ## Repos:
  ## - List of projects to pull from
  Repos = ["abc", "def", "ghi"]
 `
)

type Gitlab struct {
	client      *gitlab.Client
	ctx         context.Context
	wg          *sync.WaitGroup
	cancel      context.CancelFunc
	currentPage int
	projects    map[int]string
	Token       string
	Endpoint    string
	Repos       []string
}

// Description - display description
func (h *Gitlab) Description() string { return description }

// SampleConfig - generate configuretion
func (h *Gitlab) SampleConfig() string { return sampleConfig }

// Gather - Main code responsible for gathering, processing and creating metrics
func (h *Gitlab) Gather(acc telegraf.Accumulator) error {
	return nil
}

func (h *Gitlab) Stop() {
	h.cancel()
	h.wg.Wait()
}

func (h *Gitlab) Start(acc telegraf.Accumulator) error {
	h.ctx, h.cancel = context.WithCancel(context.Background())
	h.wg = &sync.WaitGroup{}
	h.wg.Add(1)

	_, err := url.Parse(h.Endpoint)
	if err != nil {
		return fmt.Errorf("invalid URL \"%s\"", h.Endpoint)
	}

	h.client = gitlab.NewClient(nil, h.Token)
	h.client.SetBaseURL(h.Endpoint)
	//visibility := gitlab.InternalVisibility

	ps, resp, err := h.client.Projects.ListProjects(&gitlab.ListProjectsOptions{
		//Visibility: &visibility,
	})
	if err != nil {
		return fmt.Errorf("unable to perform HTTP client GET on \"%s\": %s", h.Endpoint, err)
	}

	h.projects = make(map[int]string)
	for _, p := range ps {
		h.projects[p.ID] = p.Name
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http status ok not met")
	}

	go h.fetchMergeRequests(acc)
	go h.fetchCommits(acc)
	return nil
}

func (h *Gitlab) fetchCommits(acc telegraf.Accumulator) {

	for _, rep := range h.Repos {

		key, ok := mapkey(h.projects, rep)
		if !ok {
			acc.AddError(fmt.Errorf("value does not exist in map"))
		}

		pp := 100
		page := 0
		for {
			commits, rs, err := h.client.Commits.ListCommits(key, &gitlab.ListCommitsOptions{
				ListOptions: gitlab.ListOptions{PerPage: pp, Page: page},
			})
			if err != nil {
				acc.AddError(fmt.Errorf("unable to list project : ", rep, err))
			}
			for _, commit := range commits {

				tmpFields := map[string]interface{}{
					"stats":        commit.Stats,
					"message":      commit.Message,
					"status":       commit.Status,
					"committed_at": commit.CommittedDate,
				}

				tmpTags := map[string]string{
					"ID":              commit.ID,
					"title":           commit.Title,
					"author_name":     commit.AuthorName,
					"author_email":    commit.AuthorEmail,
					"committer_name":  commit.CommitterName,
					"committer_email": commit.CommitterEmail,
				}
				acc.AddFields("commits", tmpFields, tmpTags, *commit.CreatedAt)
			}

			rs.Body.Close()
			page += 1
			if len(commits) < pp {
				break
			}
		}
	}
}

func (h *Gitlab) fetchMergeRequests(acc telegraf.Accumulator) {
	defer h.wg.Done()
	pp := 100
	page := 0
	for {
		mrs, rs, err := h.client.MergeRequests.ListMergeRequests(&gitlab.ListMergeRequestsOptions{
			Scope:       gitlab.String("all"),
			ListOptions: gitlab.ListOptions{PerPage: pp, Page: page},
		})

		if err != nil {
			acc.AddError(fmt.Errorf("unable to list merge requests, %+v", err))
			break
		}

		for _, mr := range mrs {

			ticketID := formatTicketID(findTicketID(mr.Title))
			mrType := getMRType(mr.Title)

			tmpFields := map[string]interface{}{
				"upvotes":     mr.Upvotes,
				"downvotes":   mr.Downvotes,
				"changes":     mr.ChangesCount,
				"updated_at":  mr.UpdatedAt,
				"notes_count": mr.UserNotesCount,
				"wip":         mr.WorkInProgress,
			}

			tmpTags := map[string]string{
				"merge_status":       mr.MergeStatus,
				"author":             mr.Author.Name,
				"username":           mr.Author.Username,
				"assignee":           mr.Assignee.Name,
				"project":            h.projects[mr.ProjectID],
				"source_project":     h.projects[mr.SourceProjectID],
				"target_project":     h.projects[mr.TargetProjectID],
				"state":              mr.State,
				"jira_ticket_id":     ticketID,
				"merge_request_type": mrType,
			}
			acc.AddFields("merge_requests", tmpFields, tmpTags, *mr.CreatedAt)
		}
		rs.Body.Close()
		page += 1
		if len(mrs) < pp {
			break
		}
	}
}

func findTicketID(s string) string {

	r, _ := regexp.Compile(`[a-zA-Z]+\W\d{2,5}`)

	return r.FindString(s)

}

func formatTicketID(s string) string {

	replacer := strings.NewReplacer("/", "-", " ", "-", ":", "-")

	return strings.ToUpper(replacer.Replace(s))
}

func getMRType(s string) string {

	r, _ := regexp.Compile(`^[a-zA-Z]+\W[a-zA-Z]+\W\d{2,5}`)

	sr := r.FindString(s)

	data := formatTicketID(sr)
	tempData := strings.Split(data, "-")
	return tempData[0]
}

func mapkey(m map[int]string, value string) (key int, ok bool) {
	for k, v := range m {
		if v == value {
			key = k
			ok = true
			return
		}
	}
	return
}

func init() {
	inputs.Add("gitlab", func() telegraf.Input { return &Gitlab{} })
}
