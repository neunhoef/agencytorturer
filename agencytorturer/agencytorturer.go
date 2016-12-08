package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

var perms = [6][3]int{
	{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0},
}

var connections = [4][]int{
	[]int{}, []int{1}, []int{2}, []int{1, 2},
}

var delays = [...][2]bool{
	{false, false},
	{false, true},
	{true, false},
	{true, true},
}

var arangodExecutable = "./build/bin/arangod"
var arangodJSstartup = "./js"

var skipCases int

func makeArgs(myDir string, myAddress string, myPort string, i int) (args []string) {
	args = make([]string, 0, 40)
	args = append(args,
		arangodExecutable,
		"-c", "none",
		"--server.endpoint", "tcp://0.0.0.0:"+myPort,
		"--database.directory", myDir+"data",
		"--javascript.startup-directory", arangodJSstartup,
		"--javascript.app-path", myDir+"apps",
		"--log.file", myDir+"arangod.log",
		"--log.level", "INFO",
		"--log.force-direct", "false",
		"--server.authentication", "false",
	)
	args = append(args,
		"--agency.activate", "true",
		"--agency.my-address", "tcp://"+myAddress+myPort,
		"--agency.size", "3",
		"--agency.supervision", "true",
		"--foxx.queues", "false",
		"--javascript.v8-contexts", "1",
		"--server.statistics", "false",
		"--server.threads", "8",
	)
	for j := 0; j < 3; j++ {
		if j != i {
			args = append(args,
				"--agency.endpoint",
				"tcp://localhost:"+strconv.Itoa(4001+j))
		}
	}
	return
}

var count int

func startAgent(i int, links []int) (agentProc *os.Process) {
	fmt.Println("Starting agent", i, "...")
	myAddress := "localhost:"
	var myPort string
	var myDir string
	var args []string

	// Start agent:
	var err error
	myPort = strconv.Itoa(4001 + i)
	myDir = strconv.Itoa(count) + "agent" + myPort + string(os.PathSeparator)
	os.MkdirAll(myDir+"data", 0755)
	os.MkdirAll(myDir+"apps", 0755)
	args = makeArgs(myDir, myAddress, myPort, i)
	agentProc, err = os.StartProcess(arangodExecutable, args,
		&os.ProcAttr{"", nil, []*os.File{os.Stdin, nil, nil}, nil})
	if err != nil {
		fmt.Println("Error whilst starting agent", i, ":", err)
	}
	return
}

func killAgent(agentProc *os.Process, i int) {
	fmt.Println("Killing agent:", i)
	myPort := strconv.Itoa(4001 + i)
	myDir := strconv.Itoa(count) + "agent" + myPort
	client := &http.Client{
		Timeout: time.Duration(15) * time.Second,
	}
	addr := "http://localhost:" + myPort
	req, e := http.NewRequest("DELETE", addr+"/_admin/shutdown", nil)
	r, e := client.Do(req)
	if e == nil && r.StatusCode == http.StatusOK {
		agentProc.Wait()
		os.RemoveAll(myDir)
		return
	}
	agentProc.Kill()
	agentProc.Wait()
	os.RemoveAll(myDir)
}

func waitAPIVersion(addr string) {
	for {
		r, e := http.Get(addr)
		if r != nil && r.Body != nil {
			defer r.Body.Close()
		}
		if e == nil && r.StatusCode == http.StatusOK {
			fmt.Println("Reached", addr+"/_api/version, good")
			return
		}
		fmt.Println("Waiting for", addr+"/_api/version", e)
		if e == nil {
			fmt.Println("StatusCode:", r.StatusCode)
		}
		time.Sleep(1000000000)
	}
}

// AgencyConfiguration describes the configuration in agency
type AgencyConfiguration struct {
	Pool     map[string]string `json:"pool"`
	Active   []string          `json:"active"`
	ID       string            `json:"id"`
	Endpoint string            `json:"endpoint"`
}

func (conf AgencyConfiguration) String() string {
	res := "Pool:{"
	for k, v := range conf.Pool {
		res += `"` + k + `":"` + v + `",`
	}
	res += "}" + "\nActive:["
	for _, s := range conf.Active {
		res += `"` + s + `",`
	}
	res += "]\nId:" + conf.ID + " Endpoint:" + conf.Endpoint
	return res
}

// AgencyControl describes the result structure for control api of agency.
type AgencyControl struct {
	Term          int                 `json:"term"`
	LeaderID      string              `json:"leaderId"`
	Configuration AgencyConfiguration `json:"configuration"`
}

func (control AgencyControl) String() string {
	return "Term:" + strconv.Itoa(control.Term) +
		" LeaderId:" + control.LeaderID +
		"\nConfiguration:" + control.Configuration.String()
}

func waitAPIAgencyConfig(addr string, LeaderID *string) {
	for {
		client := &http.Client{
			Timeout: 15 * time.Second,
		}
		r, e := client.Get(addr + "/_api/agency/config")
		if e == nil && r.StatusCode == http.StatusOK {
			body, _ := ioutil.ReadAll(r.Body)
			r.Body.Close()
			var control AgencyControl
			json.Unmarshal(body, &control)
			//fmt.Println("\nAgencyControl:", control)
			if len(control.Configuration.Pool) < 3 {
				fmt.Println("Only", len(control.Configuration.Pool), "agents in pool...")
			} else if len(control.Configuration.Active) < 3 {
				fmt.Println("Only", len(control.Configuration.Active), "active agents...")
			} else if control.LeaderID == "" {
				fmt.Println("No leader yet...")
			} else {
				if *LeaderID == "" {
					*LeaderID = control.LeaderID
					fmt.Println("Agent", addr, "sane.")
					return
				} else if *LeaderID == control.LeaderID {
					fmt.Println("Agent", addr, "sane.")
					return
				}
				fmt.Println("Seeing different leader than before: old:", *LeaderID,
					"new:", control.LeaderID, "starting from scratch...")
				*LeaderID = ""
				return
			}
			time.Sleep(1000000000)
			continue
		}
		fmt.Println("Waiting for", addr+"/_api/agency/config", e)
		if e == nil {
			fmt.Println("StatusCode:", r.StatusCode)
		}
		time.Sleep(1000000000)
	}
}

func testAgency() {
	var leaderID string
	waitAPIVersion("http://localhost:4001")
	waitAPIVersion("http://localhost:4002")
	waitAPIVersion("http://localhost:4003")
	for i := 0; i < 3; i++ {
		port := strconv.Itoa(4001 + i)
		waitAPIAgencyConfig("http://localhost:"+port, &leaderID)
		if leaderID == "" {
			i = -1
		}
	}
}

func doCase(perm [3]int, graph [3][]int, delay [2]bool) {
	count++
	fmt.Println("\nCase", count, ":", perm, graph, delay)
	var a [3]*os.Process
	for i := 0; i < 3; i++ {
		a[i] = startAgent(perm[i], graph[perm[i]])
		if i < 2 && delay[i] {
			time.Sleep(1000000000)
		}
	}

	testAgency()

	var wg sync.WaitGroup
	wg.Add(3)
	for i := 2; i >= 0; i-- {
		go func(ii int) {
			killAgent(a[ii], perm[ii])
			wg.Done()
		}(i)
	}
	wg.Wait()
}

func isConnected(graph [3][]int) bool {
	var link [3][3]bool // all false
	for i := 0; i < 3; i++ {
		for _, j := range graph[i] {
			link[i][j] = true
			link[j][i] = true
		}
	}
	tab := make([]int, 1, 3)
	var seen = [3]bool{true, false, false}
	for i := 0; i < len(tab); i++ {
		p := tab[i]
		for j := 0; j < 3; j++ {
			if j != p && link[p][j] && !seen[j] {
				tab = append(tab, j)
				seen[j] = true
			}
		}
	}
	return len(tab) == 3
}

func main() {
	flag.IntVar(&skipCases, "skip", skipCases, "cases to skip")
	flag.Parse()
	count += skipCases
	counter := 0
	for i := 0; i < len(perms); i++ {
		var graph [3][]int
		for c1 := 0; c1 < len(connections); c1++ {
			graph[0] = make([]int, 0, 2)
			for _, d := range connections[c1] {
				graph[0] = append(graph[0], d)
			}
			for c2 := 0; c2 < len(connections); c2++ {
				graph[1] = make([]int, 0, 2)
				for _, d := range connections[c2] {
					graph[1] = append(graph[1], (d+1)%3)
				}
				for c3 := 0; c3 < len(connections); c3++ {
					graph[2] = make([]int, 0, 2)
					for _, d := range connections[c3] {
						graph[2] = append(graph[2], (d+2)%3)
					}
					if isConnected(graph) {
						for j := 0; j < len(delays); j++ {
							counter++
							if counter > skipCases {
								doCase(perms[i], graph, delays[j])
							}
						}
					}
				}
			}
		}
	}
}
