package gitlab

import (
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf"
	"github.com/xanzy/go-gitlab"
	"fmt"
	"net/http"
	"net/url"
	"context"
	"sync"
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
	ctx context.Context
	wg *sync.WaitGroup
	cancel context.CancelFunc
	currentPage int
	projects map[int]string
	Token string
	Endpoint string
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

	ps, resp, err := h.client.Projects.ListProjects(&gitlab.ListProjectsOptions{})
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
	return nil
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
			tmpFields := map[string]interface{}{
				"upvotes":     mr.Upvotes,
				"downvotes":   mr.Downvotes,
				"changes":     mr.ChangesCount,
				"updated_at":  mr.UpdatedAt,
				"notes_count": mr.UserNotesCount,
				"wip":         mr.WorkInProgress,
			}

			tmpTags := map[string]string{
				"merge_status": mr.MergeStatus,
				"author":       mr.Author.Name,
				"username":     mr.Author.Username,
				"assignee":     mr.Assignee.Name,
				"project":      h.projects[mr.ProjectID],
				"state":        mr.State,
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

func init() {
	inputs.Add("gitlab", func() telegraf.Input { return &Gitlab{} })
}

