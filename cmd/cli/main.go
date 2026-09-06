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
	"apirequest/backend/convert"
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
	case "export":
		os.Exit(cmdExport(os.Args[2:]))
	case "import":
		os.Exit(cmdImport(os.Args[2:]))
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

  apirequest-cli export --collection <name|id> --format <fmt> [flags]
      Export a collection headlessly (Postman/OpenAPI/cURL/HAR/Insomnia/.http...).

  apirequest-cli import --file <path> [flags]
      Import a file headlessly into a workspace (same formats as the desktop importer).

Import flags:
  --file          input file path (required)
  --format        import format id; omit for auto-detection
  --workspace     target workspace name or id (default: first workspace)
  --db            app data dir override (default: OS config dir)

Import prints a JSON summary (collectionId, name, requests, environments) to stdout.

Export flags:
  --collection    collection name or id (required)
  --format        export format id (postman | openapi | openapi3.1 | swagger2 |
                  curl | restclient | har | insomnia)
  --out           write to this file instead of stdout
  --workspace     workspace name or id (default: first workspace)
  --db            app data dir override (default: OS config dir)

Export output is redacted: secret values never leave the local vault.

Run flags:
  --collection    collection name or id (required)
  --workspace     workspace name or id (default: first workspace)
  --data          CSV/JSON data file path (per-row iteration)
  --iterations    iteration count when no data file (default 1)
  --delay         milliseconds between requests (think-time; 0 = none)
  --stop-on-error stop at first failure
  --report        also write JSON report to this file
  --junit         also write a JUnit XML report to this file (CI test reporting)
  --env           environment name or id (default: the workspace's active environment)
  --env-file      JSON file of variables ({"KEY": "value"}); data-file rows override it
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

// cmdExport 无头导出：复用桌面端 ConvertApi（含 collectTree + 脱敏），
// 输出走 stdout 或 --out 文件。退出码 2 = 用法/找不到目标
// cmdImport 无头导入：解析文件 → ImportNodeTree 落库（建议环境一并创建，不激活）。
// 语义对齐桌面端 ConvertApi.ImportCommit；stdout 输出 JSON 摘要供脚本消费
func cmdImport(args []string) int {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	file := fs.String("file", "", "")
	format := fs.String("format", "", "")
	workspace := fs.String("workspace", "", "")
	dbDir := fs.String("db", "", "")
	fs.Parse(args)

	if *file == "" {
		fmt.Fprintln(os.Stderr, "--file is required")
		return 2
	}
	payload, err := os.ReadFile(*file)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read file:", err)
		return 2
	}

	store, err := openStore(*dbDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open store:", err)
		return 2
	}
	defer store.Close()

	wsId, _, err := resolveTargetWorkspace(store, *workspace)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	res, err := convert.Import(*format, string(payload))
	if err != nil {
		fmt.Fprintln(os.Stderr, "import:", err)
		return 2
	}
	saved, err := store.ImportNodeTree(wsId, res.Collection, res.Children)
	if err != nil {
		fmt.Fprintln(os.Stderr, "commit:", err)
		return 2
	}
	envs := []string{}
	for _, sug := range res.SuggestedEnvironments {
		env, envErr := store.UpsertEnvironment(model.Environment{
			WorkspaceId: wsId, Name: sug.Name, Variables: sug.Variables,
		})
		if envErr != nil {
			fmt.Fprintln(os.Stderr, "create environment:", envErr)
			return 2
		}
		envs = append(envs, env.Name)
	}
	for _, w := range res.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
	requests := 0
	for _, n := range res.Children {
		if n.Kind == "request" {
			requests++
		}
	}
	out := struct {
		CollectionId string   `json:"collectionId"`
		Name         string   `json:"name"`
		Requests     int      `json:"requests"`
		Environments []string `json:"environments"`
	}{CollectionId: saved.Id, Name: saved.Name, Requests: requests, Environments: envs}
	encoded, _ := json.Marshal(out)
	fmt.Println(string(encoded))
	return 0
}

func cmdExport(args []string) int {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	collection := fs.String("collection", "", "")
	format := fs.String("format", "postman", "")
	outPath := fs.String("out", "", "")
	workspace := fs.String("workspace", "", "")
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

	_, colId, err := resolveTarget(store, *workspace, *collection)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	// ConvertApi.ExportData 已走 redactExportTree：密钥值不落导出产物
	api := binding.NewConvertApi(store)
	data, err := api.ExportData(colId, *format)
	if err != nil {
		fmt.Fprintln(os.Stderr, "export:", err)
		return 2
	}
	if *outPath != "" {
		if werr := os.WriteFile(*outPath, []byte(data), 0o600); werr != nil {
			fmt.Fprintln(os.Stderr, "write:", werr)
			return 2
		}
		fmt.Fprintf(os.Stderr, "exported %s -> %s\n", *format, *outPath)
		return 0
	}
	fmt.Print(data)
	return 0
}

func cmdRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	collection := fs.String("collection", "", "")
	workspace := fs.String("workspace", "", "")
	dataFile := fs.String("data", "", "")
	iterations := fs.Int("iterations", 1, "")
	stopOnError := fs.Bool("stop-on-error", false, "")
	reportPath := fs.String("report", "", "")
	envName := fs.String("env", "", "")
	envFile := fs.String("env-file", "", "")
	junitPath := fs.String("junit", "", "")
	dbDir := fs.String("db", "", "")
	delayMs := fs.Int("delay", 0, "")
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

	envOverrides := map[string]string{}
	if *envFile != "" {
		content, ferr := os.ReadFile(*envFile)
		if ferr != nil {
			fmt.Fprintln(os.Stderr, "read env file:", ferr)
			return 2
		}
		parsed, perr := parseEnvFile(string(content))
		if perr != nil {
			fmt.Fprintln(os.Stderr, "parse env file:", perr)
			return 2
		}
		envOverrides = parsed
	}
	envId := ""
	if *envName != "" {
		envId, err = resolveEnv(store, wsId, *envName)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}

	opts := runner.Options{
		Iterations:   *iterations,
		StopOnError:  *stopOnError,
		EnvId:        envId,
		EnvOverrides: envOverrides,
		DelayMs:      *delayMs,
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

	collectionName := *collection
	if nodes, nerr := store.ListNodes(wsId); nerr == nil {
		for _, n := range nodes {
			if n.Id == colId {
				collectionName = n.Name
				break
			}
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
	if *junitPath != "" {
		jout, jerr := runner.MarshalJUnitXML(report, collectionName)
		if jerr != nil {
			fmt.Fprintln(os.Stderr, "marshal junit:", jerr)
		} else if werr := os.WriteFile(*junitPath, jout, 0o644); werr != nil {
			fmt.Fprintln(os.Stderr, "write junit:", werr)
		}
	}

	fmt.Fprintf(os.Stderr, "\n%d passed, %d failed, %d skipped in %dms\n",
		report.Passed, report.Failed, report.Skipped, report.DurationMs)

	if report.Failed > 100 {
		return 100
	}
	return report.Failed
}

// parseEnvFile 解析 --env-file：JSON 对象，key→string 值。非字符串值显式拒绝
// （提示加引号），避免 CI 里数字/布尔被静默转成意外形态。
func parseEnvFile(content string) (map[string]string, error) {
	var raw map[string]any
	dec := json.NewDecoder(strings.NewReader(content))
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("expected a JSON object of string values: %w", err)
	}
	vars := make(map[string]string, len(raw))
	for k, v := range raw {
		str, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("environment variable %q must be a string (quote the value)", k)
		}
		vars[k] = str
	}
	return vars, nil
}

// resolveEnv 按名称或 id 解析工作区内的环境
func resolveEnv(store *storage.Store, wsId, nameOrId string) (string, error) {
	envs, err := store.ListEnvironments(wsId)
	if err != nil {
		return "", err
	}
	var matches []model.Environment
	for _, e := range envs {
		if e.Id == nameOrId || e.Name == nameOrId {
			matches = append(matches, e)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("environment not found in workspace: %s", nameOrId)
	case 1:
		return matches[0].Id, nil
	default:
		return "", fmt.Errorf("environment name %q is ambiguous (%d matches); use its id", nameOrId, len(matches))
	}
}

// resolveTargetWorkspace 按名称或 id 解析工作区（不带集合；import 场景）
func resolveTargetWorkspace(store *storage.Store, workspace string) (string, string, error) {
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
	return ws.Id, ws.Name, nil
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
