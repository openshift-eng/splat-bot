package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"

	"github.com/openshift-splat-team/splat-bot/data"
	"github.com/openshift-splat-team/splat-bot/pkg/util"

	prowv1 "sigs.k8s.io/prow/pkg/apis/prowjobs/v1"
)

const (
	prowJobsUrl      = "https://prow.ci.openshift.org/prowjobs.js?omit=annotations,decoration_config,pod_spec"
	retrievalTimeout = time.Hour + time.Minute*30
	retrieveEvery    = time.Minute * 29
)

var (
	prowTimer    *time.Timer
	prowJobList  *prowv1.ProwJobList
	periodicJobs []prowv1.ProwJob
	mu           sync.Mutex
)

var ProwGraphAttributes = data.Attributes{
	Commands:       []string{"prow", "graph"},
	RequireMention: true,
	Callback: func(ctx context.Context, client util.SlackClientInterface, evt *slackevents.MessageEvent, args []string) ([]slack.MsgOption, error) {
		startProwRetrievalTimers()

		results, err := createProwGraph(args[2])
		if err != nil {
			return nil, err
		}

		return util.StringToBlock(results, false), nil
	},
	RequiredArgs: 3,
	HelpMarkdown: "retrieve prow results: `prow graph [platform]`",
	ShouldMatch: []string{
		"prow graph vsphere",
	},
	ShouldntMatch: []string{
		"jira create-with-summary PROJECT bug",
		"jira create-with-summary PROJECT Todo",
	},
}

var ProwAttributes = data.Attributes{
	Commands:       []string{"prow", "results"},
	RequireMention: true,
	Callback: func(ctx context.Context, client util.SlackClientInterface, evt *slackevents.MessageEvent, args []string) ([]slack.MsgOption, error) {
		startProwRetrievalTimers()

		results, err := queryProwResults(args[2], args[3], prowv1.ProwJobState(args[4]))
		if err != nil {
			return nil, err
		}

		return util.StringToBlock(results, false), nil
	},
	RequiredArgs: 5,
	HelpMarkdown: "retrieve prow results: `prow results [platform] [version] [state]`",
	ShouldMatch: []string{
		"prow results vsphere 4.16 success",
	},
	ShouldntMatch: []string{
		"jira create-with-summary PROJECT bug",
		"jira create-with-summary PROJECT Todo",
	},
}

func fetchProwJobs() (*prowv1.ProwJobList, error) {
	var prowJobList prowv1.ProwJobList

	// Send HTTP GET request
	response, err := http.Get(prowJobsUrl)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	// Decode JSON response
	decoder := json.NewDecoder(response.Body)
	if err := decoder.Decode(&prowJobList); err != nil {
		return nil, err
	}

	return &prowJobList, nil
}

func startProwRetrievalTimers() {
	var err error

	if prowTimer == nil {
		mu.Lock()
		log.Print("mu.Lock()")
		prowJobList, err = fetchProwJobs()
		if err != nil {
			log.Fatal(err)
		}
		mu.Unlock()
		log.Print("mu.Unlock()")

		prowTimer = time.AfterFunc(retrieveEvery, func() {
			log.Print("time.AfterFunc")

			mu.Lock()
			log.Print("AfterFunc mu.Lock()")
			prowJobList, err = fetchProwJobs()
			if err != nil {
				log.Fatal(err)
			}
			mu.Unlock()
			log.Print("AfterFunc mu.Unlock()")
		})

		time.AfterFunc(retrievalTimeout, func() {
			mu.Lock()
			log.Print("prowTimer Stop()")
			prowTimer.Stop()
			prowJobList = nil
			mu.Unlock()
		})
	}
}

func removeJob(platform, version string, prowJobState *prowv1.ProwJobState, job prowv1.ProwJob) bool {
	if !job.Complete() {
		return true
	}
	if job.Spec.Type != prowv1.PeriodicJob {
		return true
	}
	if !strings.Contains(job.Spec.Job, "nightly") {
		return true
	}
	if !strings.Contains(job.Spec.Job, platform) {
		return true
	}
	if version != "" {
		if !strings.Contains(job.Spec.Job, version) {
			return true
		}
	}
	if prowJobState != nil {
		if job.Status.State != *prowJobState {
			return true
		}
	}

	return false
}

func createProwGraph(platform string) (string, error) {
	var resultsBuilder strings.Builder
	re, err := regexp.Compile(`(\d\.\d*)`)
	if err != nil {
		return "", err
	}

	results := make(map[string]map[prowv1.ProwJobState]string)

	mu.Lock()
	if prowJobList != nil {
		periodicJobs = make([]prowv1.ProwJob, 0)

		for _, pj := range prowJobList.Items {
			if !removeJob(platform, "", nil, pj) {
				periodicJobs = append(periodicJobs, pj)
			}
		}

		for _, j := range periodicJobs {
			versionRegex := re.FindStringSubmatch(j.Spec.Job)

			if len(versionRegex) < 1 {
				continue
			}
			ocpVersion := versionRegex[0]

			if _, ok := results[ocpVersion]; !ok {
				results[ocpVersion] = make(map[prowv1.ProwJobState]string)
			}
			switch j.Status.State {
			case prowv1.FailureState:
				results[ocpVersion][prowv1.FailureState] += "F"
			case prowv1.SuccessState:
				results[ocpVersion][prowv1.SuccessState] += "S"
			}
		}
		tbwrite := tabwriter.NewWriter(&resultsBuilder, 0, 0, 0, ' ', tabwriter.Debug)

		_, err := fmt.Fprint(tbwrite, "```\n")
		if err != nil {
			return "", err
		}
		for k, v := range results {
			_, err := fmt.Fprintf(tbwrite, "%s\t%s\t%s\t\n", k, v[prowv1.SuccessState], v[prowv1.FailureState])
			if err != nil {
				return "", err
			}
		}
		_, err = fmt.Fprint(tbwrite, "\n```")
		if err != nil {
			return "", err
		}

		err = tbwrite.Flush()
		if err != nil {
			return "", err
		}
	}
	periodicJobs = make([]prowv1.ProwJob, 0)
	mu.Unlock()
	return resultsBuilder.String(), nil
}
func queryProwResults(platform, version string, prowJobState prowv1.ProwJobState) (string, error) {
	var resultsBuilder strings.Builder
	numToRetrieve := 10

	mu.Lock()
	if prowJobList != nil {
		periodicJobs = make([]prowv1.ProwJob, 0)

		for _, pj := range prowJobList.Items {
			if !removeJob(platform, version, &prowJobState, pj) {
				periodicJobs = append(periodicJobs, pj)
			}
		}

		periodicJobLength := len(periodicJobs)
		if periodicJobLength > numToRetrieve {
			periodicJobLength = numToRetrieve
		}

		periodicJobs = periodicJobs[0:periodicJobLength:periodicJobLength]

		for _, j := range periodicJobs {
			fmt.Fprintf(&resultsBuilder, "<%s|%s>\n", j.Status.URL, j.Spec.Job)
		}
	}
	periodicJobs = make([]prowv1.ProwJob, 0)
	mu.Unlock()
	return resultsBuilder.String(), nil
}
