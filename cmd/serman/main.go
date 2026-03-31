package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"serman/internal/serman"
)

type EnvFile struct {
	lines []string
	kv    map[string]string
	idx   map[string]int
	quote map[string]byte
}

func LoadEnvFile(path string) (*EnvFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	env := &EnvFile{
		lines: lines,
		kv:    map[string]string{},
		idx:   map[string]int{},
		quote: map[string]byte{},
	}
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				env.quote[key] = val[0]
				val = val[1 : len(val)-1]
			}
		}
		env.kv[key] = val
		env.idx[key] = i
	}
	return env, nil
}

func (e *EnvFile) Get(key string) (string, bool) {
	v, ok := e.kv[key]
	return v, ok
}

func (e *EnvFile) Set(key, val string) {
	q, hasQuote := e.quote[key]
	line := formatEnvLine(key, val, q, hasQuote)
	if i, ok := e.idx[key]; ok {
		e.lines[i] = line
	} else {
		e.lines = append(e.lines, line)
		e.idx[key] = len(e.lines) - 1
	}
	e.kv[key] = val
}

func formatEnvLine(key, val string, q byte, hasQuote bool) string {
	if hasQuote {
		return fmt.Sprintf("%s=%c%s%c", key, q, val, q)
	}
	if strings.ContainsAny(val, " #") {
		return fmt.Sprintf("%s=\"%s\"", key, val)
	}
	return fmt.Sprintf("%s=%s", key, val)
}

func (e *EnvFile) Save(path string) error {
	content := strings.Join(e.lines, "\n")
	return os.WriteFile(path, []byte(content), 0644)
}

type ServiceStatus struct {
	State  string
	Health string
	Ports  string
}

type DependencyStatus struct {
	DockerOK       bool
	DockerVersion  string
	ComposeOK      bool
	ComposeVersion string
}

func main() {
	root, err := findRepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if err := ensureEnvFile(root); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	app := tview.NewApplication()
	header := tview.NewTextView().SetDynamicColors(true)
	header.SetText("[::b][#00ff9c]STACK CONTROL[white]  [gray]local services manager")

	depPanel := tview.NewTextView().SetDynamicColors(true)
	depPanel.SetBorder(true).SetTitle(" DEPENDENCIES ")

	statusBar := tview.NewTextView().SetDynamicColors(true)
	statusBar.SetText("[gray]Keys: [#00ff9c]Enter[e:edit] [#00ff9c]s[start] [#00ff9c]t[stop] [#00ff9c]r[restart] [#00ff9c]f[fresh] [#00ff9c]R[refresh] [#00ff9c]q[quit]")

	table := tview.NewTable().SetSelectable(true, false)
	table.SetTitle(" SERVICES ").SetBorder(true)

	pages := tview.NewPages()

	deps := checkDependencies()
	depsOK := deps.DockerOK && deps.ComposeOK
	updateDepPanel(depPanel, deps)
	if !depsOK {
		statusBar.SetText("[red]Docker and/or Docker Compose not available. Install them and reopen.")
	}

	reloadTable := func() {
		var statuses map[string]ServiceStatus
		if !depsOK {
			statuses = blankStatuses("unavailable")
		} else {
			var err error
			statuses, err = getComposeStatuses(root)
			if err != nil {
				statusBar.SetText(fmt.Sprintf("[red]Status error: %v", err))
				statuses = blankStatuses("error")
			}
		}
		table.Clear()
		headers := []string{"Service", "State", "Health", "Ports"}
		for c, h := range headers {
			cell := tview.NewTableCell(fmt.Sprintf("[::b][#00ff9c]%s", h)).SetSelectable(false)
			table.SetCell(0, c, cell)
		}
		for r, svc := range serman.Services {
			st := statuses[svc.Name]
			table.SetCell(r+1, 0, tview.NewTableCell(svc.Name))
			table.SetCell(r+1, 1, tview.NewTableCell(st.State))
			table.SetCell(r+1, 2, tview.NewTableCell(st.Health))
			table.SetCell(r+1, 3, tview.NewTableCell(st.Ports))
		}
	}

	reloadTable()
	table.Select(1, 0)

	openEditor := func(svc serman.ServiceConfig) {
		envPath := filepath.Join(root, ".env")
		env, err := LoadEnvFile(envPath)
		if err != nil {
			statusBar.SetText(fmt.Sprintf("[red]Failed to read .env: %v", err))
			return
		}

		defaults := map[string]string{}
		examplePath := filepath.Join(root, ".env.example")
		if ex, err := LoadEnvFile(examplePath); err == nil {
			for k, v := range ex.kv {
				defaults[k] = v
			}
		}

		form := tview.NewForm()
		form.SetBorder(true).SetTitle(fmt.Sprintf(" Edit %s ", svc.Name))

		values := map[string]*string{}
		for _, f := range svc.Env {
			key := f.Key
			val, ok := env.Get(f.Key)
			if !ok {
				val = defaults[f.Key]
			}
			v := val
			values[key] = &v
			form.AddInputField(f.Label, v, 0, nil, func(text string) {
				*values[key] = text
			})
		}

		form.AddButton("Save", func() {
			for _, f := range svc.Env {
				if v, ok := values[f.Key]; ok {
					env.Set(f.Key, *v)
				}
			}
			if err := env.Save(envPath); err != nil {
				statusBar.SetText(fmt.Sprintf("[red]Failed to save .env: %v", err))
				return
			}
			statusBar.SetText(fmt.Sprintf("[green]Saved .env. Restart %s to apply.", svc.Name))
			pages.SwitchToPage("main")
		})
		form.AddButton("Cancel", func() {
			pages.SwitchToPage("main")
		})

		pages.AddAndSwitchToPage("edit", form, true)
		app.SetFocus(form)
	}

	runAction := func(svc serman.ServiceConfig, action string) {
		var err error
		if !depsOK {
			statusBar.SetText("[red]Action blocked: Docker and Docker Compose are required.")
			return
		}
		switch action {
		case "start":
			err = compose(root, "up", "-d", svc.Name)
		case "stop":
			err = compose(root, "stop", svc.Name)
		case "restart":
			err = compose(root, "restart", svc.Name)
		case "fresh":
			err = freshService(root, svc)
		default:
			return
		}
		if err != nil {
			statusBar.SetText(fmt.Sprintf("[red]%s failed: %v", title(action), err))
		} else {
			statusBar.SetText(fmt.Sprintf("[green]%s ok: %s", title(action), svc.Name))
		}
		reloadTable()
	}

	table.SetSelectedFunc(func(row, column int) {
		if row <= 0 {
			return
		}
		svc := serman.Services[row-1]
		openEditor(svc)
	})

	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(header, 1, 0, false).
		AddItem(depPanel, 3, 0, false).
		AddItem(table, 0, 1, true).
		AddItem(statusBar, 1, 0, false)

	pages.AddPage("main", layout, true, true)

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		row, _ := table.GetSelection()
		if row <= 0 {
			return event
		}
		svc := serman.Services[row-1]
		switch event.Rune() {
		case 'q':
			app.Stop()
			return nil
		case 'e':
			openEditor(svc)
			return nil
		case 's':
			runAction(svc, "start")
			return nil
		case 't':
			runAction(svc, "stop")
			return nil
		case 'r':
			runAction(svc, "restart")
			return nil
		case 'f':
			runAction(svc, "fresh")
			return nil
		case 'R':
			reloadTable()
			return nil
		}
		return event
	})

	if err := app.SetRoot(pages, true).EnableMouse(true).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func blankStatuses(state string) map[string]ServiceStatus {
	res := map[string]ServiceStatus{}
	for _, svc := range serman.Services {
		res[svc.Name] = ServiceStatus{State: state}
	}
	return res
}

func updateDepPanel(view *tview.TextView, deps DependencyStatus) {
	statusLine := func(label string, ok bool, version string) string {
		if ok {
			return fmt.Sprintf("[green]●[white] %s [gray]%s", label, version)
		}
		return fmt.Sprintf("[red]●[white] %s [red]not found", label)
	}
	lines := []string{
		statusLine("Docker", deps.DockerOK, deps.DockerVersion),
		statusLine("Docker Compose", deps.ComposeOK, deps.ComposeVersion),
	}
	view.SetText(strings.Join(lines, "\n"))
}

func checkDependencies() DependencyStatus {
	deps := DependencyStatus{}
	if out, err := exec.Command("docker", "--version").CombinedOutput(); err == nil {
		deps.DockerOK = true
		deps.DockerVersion = strings.TrimSpace(string(out))
	}
	if out, err := exec.Command("docker", "compose", "version").CombinedOutput(); err == nil {
		deps.ComposeOK = true
		deps.ComposeVersion = strings.TrimSpace(string(out))
	}
	return deps
}

func findRepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for i := 0; i < 10; i++ {
		composePath := filepath.Join(wd, "docker-compose.yaml")
		servicesDir := filepath.Join(wd, "services")
		if _, err := os.Stat(composePath); err == nil {
			return wd, nil
		}
		if info, err := os.Stat(servicesDir); err == nil && info.IsDir() {
			return wd, nil
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			break
		}
		wd = parent
	}
	return "", errors.New("docker-compose.yaml not found")
}

func ensureEnvFile(root string) error {
	p := filepath.Join(root, ".env")
	if _, err := os.Stat(p); err == nil {
		return nil
	}
	ex := filepath.Join(root, ".env.example")
	b, err := os.ReadFile(ex)
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0644)
}

func getComposeStatuses(root string) (map[string]ServiceStatus, error) {
	out, err := composeOutput(root, "ps", "--format", "json")
	if err != nil {
		return map[string]ServiceStatus{}, err
	}

	raw, err := parseComposePS(out)
	if err != nil {
		return map[string]ServiceStatus{}, err
	}
	res := map[string]ServiceStatus{}
	for _, r := range raw {
		svc := stringField(r, "Service")
		if svc == "" {
			svc = stringField(r, "Name")
		}
		if svc == "" {
			continue
		}
		state := stringField(r, "State")
		if state == "" {
			state = stringField(r, "Status")
		}
		health := stringField(r, "Health")
		ports := formatPorts(r)
		res[svc] = ServiceStatus{State: state, Health: health, Ports: ports}
	}
	// fill missing
	for _, svc := range serman.Services {
		if _, ok := res[svc.Name]; !ok {
			res[svc.Name] = ServiceStatus{State: "down"}
		}
	}
	return res, nil
}

func parseComposePS(raw []byte) ([]map[string]interface{}, error) {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return nil, nil
	}

	var arr []map[string]interface{}
	if err := json.Unmarshal([]byte(text), &arr); err == nil {
		return arr, nil
	}

	var single map[string]interface{}
	if err := json.Unmarshal([]byte(text), &single); err == nil {
		return []map[string]interface{}{single}, nil
	}

	lines := strings.Split(text, "\n")
	items := make([]map[string]interface{}, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var item map[string]interface{}
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			return nil, fmt.Errorf("failed to parse docker compose ps output line %q: %w", line, err)
		}
		items = append(items, item)
	}
	return items, nil
}

func stringField(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func formatPorts(m map[string]interface{}) string {
	pubs, ok := m["Publishers"]
	if !ok {
		return ""
	}
	arr, ok := pubs.([]interface{})
	if !ok {
		return ""
	}
	parts := []string{}
	for _, a := range arr {
		pm, ok := a.(map[string]interface{})
		if !ok {
			continue
		}
		pub := toInt(pm["PublishedPort"])
		tgt := toInt(pm["TargetPort"])
		if pub > 0 && tgt > 0 {
			parts = append(parts, fmt.Sprintf("%d->%d", pub, tgt))
		}
	}
	return strings.Join(parts, ", ")
}

func toInt(v interface{}) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case string:
		var n int
		_, _ = fmt.Sscanf(t, "%d", &n)
		return n
	default:
		return 0
	}
}

func compose(root string, args ...string) error {
	_, err := composeOutput(root, args...)
	return err
}

func composeOutput(root string, args ...string) ([]byte, error) {
	composeArgs, err := buildComposeArgs(root, args...)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command("docker", composeArgs...)
	cmd.Dir = root
	cmd.Stdin = os.Stdin
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("docker compose %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func buildComposeArgs(root string, args ...string) ([]string, error) {
	envPath := filepath.Join(root, ".env")
	if _, err := os.Stat(envPath); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	composeArgs := []string{"compose", "--project-directory", root}
	if _, err := os.Stat(envPath); err == nil {
		composeArgs = append(composeArgs, "--env-file", envPath)
	}

	if _, err := os.Stat(filepath.Join(root, "docker-compose.yaml")); err == nil {
		composeArgs = append(composeArgs, "-f", filepath.Join(root, "docker-compose.yaml"))
	} else {
		composeFiles, err := resolveComposeFiles(root)
		if err != nil {
			return nil, err
		}
		for _, path := range composeFiles {
			composeArgs = append(composeArgs, "-f", path)
		}
	}

	composeArgs = append(composeArgs, args...)
	return composeArgs, nil
}

func resolveComposeFiles(root string) ([]string, error) {
	dir := filepath.Join(root, "services")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.New("services directory missing")
		}
		return nil, err
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".yaml") {
			files = append(files, filepath.Join(dir, name))
		}
	}
	if len(files) == 0 {
		return nil, errors.New("no compose files found in services")
	}
	sort.Strings(files)
	return files, nil
}

func freshService(root string, svc serman.ServiceConfig) error {
	_ = compose(root, "stop", svc.Name)
	_ = compose(root, "rm", "-f", "-s", svc.Name)

	for _, v := range svc.Volumes {
		if err := removeVolume(v); err != nil {
			return err
		}
	}
	// For tmpfs services, just recreate
	return compose(root, "up", "-d", svc.Name)
}

func removeVolume(name string) error {
	cmd := exec.Command("docker", "volume", "rm", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Ignore missing volume
		if strings.Contains(string(out), "No such volume") {
			return nil
		}
		return fmt.Errorf("docker volume rm %s: %w\n%s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func title(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
