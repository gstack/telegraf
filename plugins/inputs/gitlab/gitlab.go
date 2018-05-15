package gitlab

import (
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf"
	"github.com/xanzy/go-gitlab"
	"fmt"
	"net/http"
	"net/url"
)

const (
	measurement  = "gitlab"
	description  = "Read metrics exposed by fluentd in_monitor plugin"
	sampleConfig = `
  ## This plugin reads information exposed by fluentd (using /api/plugins.json endpoint).
  ##
  ## Endpoint:
  ## - only one URI is allowed
  ## - https is not supported
  endpoint = "https://gitlab.com"
  token = "abcdefgh1234"
`
)

type Gitlab struct {
	client *gitlab.Client
	Token string
	Endpoint string
}

// Description - display description
func (h *Gitlab) Description() string { return description }

// SampleConfig - generate configuretion
func (h *Gitlab) SampleConfig() string { return sampleConfig }

// Gather - Main code responsible for gathering, processing and creating metrics
func (h *Gitlab) Gather(acc telegraf.Accumulator) error {
	_, err := url.Parse(h.Endpoint)
	if err != nil {
		return fmt.Errorf("invalid URL \"%s\"", h.Endpoint)
	}

	if h.client == nil {
		//tr := &http.Transport{
		//	ResponseHeaderTimeout: time.Duration(3 * time.Second),
		//}
		//
		//hc := &http.Client{
		//	Transport: tr,
		//	Timeout:   time.Duration(4 * time.Second),
		//}

		h.client = gitlab.NewClient(nil, h.Token)
		h.client.SetBaseURL(h.Endpoint)
	}

	ps, resp, err := h.client.Projects.ListProjects(&gitlab.ListProjectsOptions{})
	if err != nil {
		return fmt.Errorf("unable to perform HTTP client GET on \"%s\": %s", h.Endpoint, err)
	}
	defer resp.Body.Close()

	projects := make(map[int]string)
	for _, p := range ps {
		projects[p.ID] = p.Name
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http status ok not met")
	}

	cp := 0
getmr:
	mrs, rs, err := h.client.MergeRequests.ListMergeRequests(&gitlab.ListMergeRequestsOptions{
		Scope: gitlab.String("all"),
		ListOptions: gitlab.ListOptions{PerPage: 100, Page: cp},
	})

	if err != nil {
		return fmt.Errorf("unable to list merge requests, %+v", err)
	}

	fmt.Print(len(mrs))
	for _, mr := range mrs {
		tmpFields := map[string]interface{}{
			"upvotes":   mr.Upvotes,
			"downvotes": mr.Downvotes,
			"changes":   mr.ChangesCount,
			"changes_two": len(mr.Changes),
			"wip": mr.WorkInProgress,
		}

		tmpTags := map[string]string{
			"author": mr.Author.Name,
			"username": mr.Author.Username,
			"assignee": mr.Assignee.Name,
			"project": projects[mr.ProjectID],
			"state": mr.State,
		}
		acc.AddFields("merge_requests", tmpFields, tmpTags, *mr.CreatedAt)
	}
	rs.Body.Close()
	if len(mrs) == 100 {
		cp += 1
		goto getmr
	}

	rs.Body.Close()

	return nil
}

func init() {
	inputs.Add("gitlab", func() telegraf.Input { return &Gitlab{} })
}

