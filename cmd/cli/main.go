// apirequest-cli：无头运行器（docs/decisions.md OPEN-001）。
// 与桌面应用复用同一 core（storage/httpengine/binding），读同一份本地库；
// 退出码 = 失败请求数（上限 100），供 CI 判定。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"apirequest/backend/binding"
	"apirequest/backend/httpengine"
	"apirequest/backend/model"
	"apirequest/backend/platform"
	"apirequest/backend/runner"
	"apirequest/backend/storage"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "run":
		os.Exit(cmdRun(os.Args[2:]))
	case "list":
		os.Exit(cmdList(os.Args[2:]))
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `apirequest-cli — headless collection runner

Usage:
  apirequest-cli list [--db <dir>]
      List workspaces and collections.

  apirequest-cli run --collection <name|id> [flags]
      Run a collection and print a JSON report to stdout.

Run flags:
  --collection    collection name or id (required)
  --workspace     workspace name or id (default: first workspace)
  --data          CSV/JSON data file path (per-row iteration)
  --iterations    iteration count when no data file (default 1)
  --stop-on-error stop at first failure
  --report        also write JSON report to this file
  --db            app data dir override (default: OS config dir)

Exit code: number of failed requests (capped at 100); 2 = usage/setup error.
`)
}

func openStore(dataDir string) (*storage.Store, error) {
	var paths platform.Paths
	var err error
	if dataDir == "" {
		paths, err = platform.ResolvePaths()
	} else {
		paths, err = platform.EnsurePaths(dataDir)
	}
	if err != nil {
		return nil, err
	}
	return storage.Open(paths.Data)
}

func cmdList(args []string) int {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	dbDir := fs.String("db", "", "")
	fs.Parse(args)

	store, err := openStore(*dbDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open store:", err)
		return 2
	}
	defer store.Close()

	workspaces, err := store.ListWorkspaces()
	if err != nil {
		fmt.Fprintln(os.Stderr, "list workspaces:", err)
		return 2
	}
	for _, w := range workspaces {
		fmt.Printf("workspace: %s  (%s)\n", w.Name, w.Id)
		nodes, err := store.ListNodes(w.Id)
		if err != nil {
			continue
		}
		for _, n := range nodes {
			if n.Kind == "collection" {
				count := 0
				for _, c := range nodes {
					if c.Kind == "request" && isDescendant(nodes, c, n.Id) {
						count++
					}
				}
				fmt.Printf("  collection: %-24s %d requests  (%s)\n", n.Name, count, n.Id)
			}
		}
	}
	return 0
}

func isDescendant(nodes []model.Node, n model.Node, ancestorId string) bool {
	byId := map[string]model.Node{}
	for _, x := range nodes {
		byId[x.Id] = x
	}
	cur := n
	for cur.ParentId != "" {
		if cur.ParentId == ancestorId {
			return true
		}
		parent, ok := byId[cur.ParentId]
		if !ok {
			return false
		}
		cur = parent
	}
	return false
}

func cmdRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	collection := fs.String("collection", "", "")
	workspace := fs.String("workspace", "", "")
	dataFile := fs.String("data", "", "")
	iterations := fs.Int("iterations", 1, "")
	stopOnError := fs.Bool("stop-on-error", false, "")
	reportPath := fs.String("report", "", "")
	dbDir := fs.String("db", "", "")
	fs.Parse(args)

	if *collection == "" {
		fmt.Fprintln(os.Stderr, "--collection is required")
		return 2
	}

	store, err := openStore(*dbDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open store:", err)
		return 2
	}
	defer store.Close()

	wsId, colId, err := resolveTarget(store, *workspace, *collection)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	opts := runner.Options{
		Iterations:  *iterations,
		StopOnError: *stopOnError,
	}
	if *dataFile != "" {
		content, err := os.ReadFile(*dataFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "read data file:", err)
			return 2
		}
		opts.DataFile = string(content)
		if strings.HasSuffix(strings.ToLower(*dataFile), ".json") {
			opts.DataFormat = "json"
		} else {
			opts.DataFormat = "csv"
		}
	}

	engine := httpengine.New()
	engine.SetBlobsDir(store.BlobsDir())
	requestApi := binding.NewRequestApi(engine, store)
	runnerApi := binding.NewRunnerApi(requestApi, store)

	report, err := runnerApi.RunCollection("cli", wsId, colId, opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "run:", err)
		return 2
	}

	out, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(out))
	if *reportPath != "" {
		if werr := os.WriteFile(*reportPath, append(out, '\n'), 0o644); werr != nil {
			fmt.Fprintln(os.Stderr, "write report:", werr)
		}
	}
	fmt.Fprintf(os.Stderr, "\n%d passed, %d failed, %d skipped in %dms\n",
		report.Passed, report.Failed, report.Skipped, report.DurationMs)

	if report.Failed > 100 {
		return 100
	}
	return report.Failed
}

// resolveTarget 按名称或 id 解析工作区与集合
func resolveTarget(store *storage.Store, workspace, collection string) (wsId, colId string, err error) {
	workspaces, err := store.ListWorkspaces()
	if err != nil || len(workspaces) == 0 {
		return "", "", fmt.Errorf("no workspaces found (has the desktop app been run once?)")
	}
	var ws *model.Workspace
	if workspace == "" {
		ws = &workspaces[0]
	} else {
		for i := range workspaces {
			if workspaces[i].Id == workspace || workspaces[i].Name == workspace {
				ws = &workspaces[i]
				break
			}
		}
		if ws == nil {
			return "", "", fmt.Errorf("workspace not found: %s", workspace)
		}
	}

	nodes, err := store.ListNodes(ws.Id)
	if err != nil {
		return "", "", err
	}
	var matches []model.Node
	for _, n := range nodes {
		if n.Kind == "collection" && (n.Id == collection || n.Name == collection) {
			matches = append(matches, n)
		}
	}
	switch len(matches) {
	case 0:
		return "", "", fmt.Errorf("collection not found in workspace %q: %s", ws.Name, collection)
	case 1:
		return ws.Id, matches[0].Id, nil
	default:
		return "", "", fmt.Errorf("collection name %q is ambiguous (%d matches); use its id", collection, len(matches))
	}
}
