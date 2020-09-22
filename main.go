package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/eparis/bugzilla"
	log "github.com/sirupsen/logrus"
)

const (
	currentRelease = "4.6.0"
	nextRelease    = "4.7.0"

	maxBugs = 3
)

var (
	team = map[string]struct{}{
		"chuffman": struct{}{},
		"fbertina": struct{}{},
		"hekumar":  struct{}{},
		"jsafrane": struct{}{},
		"tsmetana": struct{}{},
	}

	// List of fields included in Search() output when no Query.IncludeFields is used.
	defaultFields = []string{"actual_time", "alias", "assigned_to", "assigned_to_detail", "blocks", "cc", "cc_detail", "cf_build_id", "cf_clone_of", "cf_conditional_nak", "cf_cust_facing", "cf_devel_whiteboard", "cf_doc_type", "cf_environment", "cf_fixed_in", "cf_internal_whiteboard", "cf_last_closed", "cf_partner", "cf_pgm_internal", "cf_pm_score", "cf_qa_whiteboard", "cf_qe_conditional_nak", "cf_release_notes", "cf_target_upstream_version", "cf_verified", "classification", "component", "creation_time", "creator", "creator_detail", "deadline", "depends_on", "docs_contact", "dupe_of", "estimated_time", "groups", "id", "is_cc_accessible", "is_confirmed", "is_creator_accessible", "is_open", "keywords", "last_change_time", "op_sys", "platform", "priority", "product", "qa_contact", "qa_contact_detail", "remaining_time", "resolution", "see_also", "severity", "status", "summary", "target_milestone", "target_release", "url", "version", "whiteboard"}
)

type bugState struct {
	bug          *bugzilla.Bug
	ignored      bool
	ignoreReason string
}

type bugStateArray []bugState

func (b bugStateArray) Len() int {
	return len(b)
}

func (b bugStateArray) Less(i, j int) bool {
	if !b[i].ignored && b[j].ignored {
		return true
	}
	if b[i].ignored && !b[j].ignored {
		return false
	}

	severityCode := map[string]int{
		"urgent": 1,
		"high":   2,
		"---":    3,
		"medium": 4,
		"low":    5,
	}
	if severityCode[b[i].bug.Severity] < severityCode[b[j].bug.Severity] {
		return true
	}
	return b[i].bug.ID < b[j].bug.ID
}

func (b bugStateArray) Swap(i, j int) {
	b[i], b[j] = b[j], b[i]
}

func main() {
	verbose := flag.Bool("v", false, "verbose output")
	flag.Parse()
	if *verbose {
		log.SetLevel(log.DebugLevel)
	}

	apiKey := os.Getenv("BUGZILLA_API_KEY")
	if len(apiKey) == 0 {
		log.Errorf("BUGZILLA_API_KEY environment variable must be set (https://bugzilla.redhat.com/userprefs.cgi?tab=apikey)")
		os.Exit(1)
	}

	client := bugzilla.NewClient(func() []byte {
		return []byte(apiKey)
	}, "https://bugzilla.redhat.com")

	query := bugzilla.Query{
		Product:       []string{"OpenShift Container Platform"},
		Component:     []string{"Storage"},
		Status:        []string{"NEW", "ASSIGNED", "POST", "ON_DEV"},
		TargetRelease: []string{"---", currentRelease, nextRelease},
		// When IncludeFields is set, Search returns *only* those fields - so add the default ones to get something usable.
		IncludeFields: append(defaultFields, "flags", "external_bugs"),
	}

	bugs, err := client.Search(query)
	if err != nil {
		log.Errorf("Failed to list bugs: %s", err)
		os.Exit(1)
	}

	bugStates := make(map[string]bugStateArray)
	assignees := []string{}
	for _, bug := range bugs {
		ignore, reason := ignoreBug(bug)

		if _, found := bugStates[bug.AssignedTo]; !found {
			bugStates[bug.AssignedTo] = []bugState{}
			assignees = append(assignees, bug.AssignedTo)
		}
		bugStates[bug.AssignedTo] = append(bugStates[bug.AssignedTo], bugState{
			bug:          bug,
			ignored:      ignore,
			ignoreReason: reason,
		})
	}

	sort.Strings(assignees)
	var total, totalIgnored int

	for _, assignee := range assignees {
		bugIDs := []string{}
		ignored := 0
		bugs := bugStates[assignee]
		sort.Sort(bugs)
		for _, bugState := range bugs {
			if bugState.ignored {
				ignored++
				totalIgnored++
			}
			total++
			bugIDs = append(bugIDs, strconv.Itoa(bugState.bug.ID))
		}
		fmt.Printf("%s: %d/%d: https://bugzilla.redhat.com/buglist.cgi?f1=bug_id&list_id=11351541&o1=anyexact&v1=%s\n", assignee, len(bugs)-ignored, len(bugs), strings.Join(bugIDs, "%2C"))
		for _, bugState := range bugs {
			reason := ""
			if bugState.ignored {
				reason = "[" + bugState.ignoreReason + "]"
			}
			fmt.Printf("\t %s %s https://bugzilla.redhat.com/show_bug.cgi?id=%d\n", reason, bugState.bug.Status, bugState.bug.ID)
		}
		fmt.Printf("\n")
	}
	fmt.Printf("Total: %d/%d\n", total-totalIgnored, total)
}

func ignoreBug(bug *bugzilla.Bug) (bool, string) {
	if bug.Severity == "low" {
		return true, "low"
	}

	for _, flag := range bug.Flags {
		requesteeIsOutside := true
		if _, found := team[flag.Requestee]; found {
			requesteeIsOutside = false
		}
		if flag.Name == "needinfo" && requesteeIsOutside {
			return true, fmt.Sprintf("needinfo:%s", flag.Requestee)
		}
	}

	return false, ""
}
