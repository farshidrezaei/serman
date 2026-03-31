package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"serman/internal/serman"

	"gopkg.in/yaml.v3"
)

type DependencyStatus struct {
	DockerOK       bool   `json:"dockerOk"`
	DockerVersion  string `json:"dockerVersion,omitempty"`
	ComposeOK      bool   `json:"composeOk"`
	ComposeVersion string `json:"composeVersion,omitempty"`
}

type ServiceStatus struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	Health string `json:"health,omitempty"`
	Ports  string `json:"ports,omitempty"`
}

type actionRequest struct {
	Service string `json:"service"`
	Action  string `json:"action"`
}

type serviceConfigField struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value string `json:"value"`
}

type serviceConfigResponse struct {
	Service string               `json:"service"`
	Fields  []serviceConfigField `json:"fields"`
}

type serviceConfigUpdateRequest struct {
	Service string            `json:"service"`
	Values  map[string]string `json:"values"`
}

type EnvFile struct {
	lines []string
	kv    map[string]string
	idx   map[string]int
	quote map[string]byte
}

func main() {
	root, err := findRepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if err := runCLI(root, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runCLI(root string, args []string) error {
	command := "help"
	if len(args) > 0 {
		command = strings.TrimSpace(args[0])
	}

	switch command {
	case "start":
		return startBackgroundServer(root)
	case "stop":
		return stopBackgroundServer(root)
	case "status":
		return printServerStatus(root)
	case "help", "--help", "-h":
		printHelp()
		return nil
	case "serve":
		return runServer(root)
	default:
		return fmt.Errorf("unknown command %q\n\n%s", command, helpText())
	}
}

func runServer(root string) error {
	if err := ensureRuntimeDir(root); err != nil {
		return err
	}
	if err := writePIDFile(pidFilePath(root), os.Getpid()); err != nil {
		return err
	}
	defer os.Remove(pidFilePath(root))

	mux := http.NewServeMux()
	mux.HandleFunc("/api/deps", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, checkDependencies())
	})

	mux.HandleFunc("/api/services", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := ensureComposeFiles(root); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		deps := checkDependencies()
		if !deps.DockerOK || !deps.ComposeOK {
			services := unavailableServices(root)
			writeJSON(w, services)
			return
		}
		services, err := getComposeServices(root)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, services)
	})

	mux.HandleFunc("/api/action", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		deps := checkDependencies()
		if !deps.DockerOK || !deps.ComposeOK {
			http.Error(w, "docker and docker compose are required", http.StatusBadRequest)
			return
		}

		var req actionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		req.Service = strings.TrimSpace(req.Service)
		req.Action = strings.TrimSpace(req.Action)
		if req.Service == "" || req.Action == "" {
			http.Error(w, "service and action are required", http.StatusBadRequest)
			return
		}
		if !isAllowedAction(req.Action) {
			http.Error(w, "invalid action", http.StatusBadRequest)
			return
		}
		if !isKnownService(root, req.Service) {
			http.Error(w, "unknown service", http.StatusNotFound)
			return
		}

		if err := runAction(root, req.Service, req.Action); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			service := strings.TrimSpace(r.URL.Query().Get("service"))
			if service == "" {
				http.Error(w, "service is required", http.StatusBadRequest)
				return
			}
			cfg, err := readServiceConfig(root, service)
			if err != nil {
				status := http.StatusInternalServerError
				if errors.Is(err, os.ErrNotExist) {
					status = http.StatusNotFound
				}
				http.Error(w, err.Error(), status)
				return
			}
			writeJSON(w, cfg)
		case http.MethodPost:
			var req serviceConfigUpdateRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			req.Service = strings.TrimSpace(req.Service)
			if req.Service == "" {
				http.Error(w, "service is required", http.StatusBadRequest)
				return
			}
			if err := updateServiceConfig(root, req.Service, req.Values); err != nil {
				status := http.StatusInternalServerError
				if errors.Is(err, os.ErrNotExist) {
					status = http.StatusNotFound
				}
				http.Error(w, err.Error(), status)
				return
			}
			writeJSON(w, map[string]string{"status": "ok"})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	staticDir := filepath.Join(root, "web", "dist")
	if _, err := os.Stat(staticDir); err == nil {
		mux.Handle("/", spaHandler(staticDir))
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "Web UI not built. Run: cd web && npm install && npm run build\n")
		})
	}

	addr := ":8080"
	srv := &http.Server{
		Addr:         addr,
		Handler:      withLogging(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	fmt.Printf("web ui running on http://localhost%s\n", addr)
	go openBrowser("http://localhost" + addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}

func startBackgroundServer(root string) error {
	pidPath := pidFilePath(root)
	if pid, ok := readRunningPID(pidPath); ok {
		fmt.Printf("serman-web is already running (pid %d)\n", pid)
		fmt.Printf("url: http://localhost:8080\n")
		fmt.Printf("log: %s\n", logFilePath(root))
		return nil
	}

	if err := ensureRuntimeDir(root); err != nil {
		return err
	}

	logPath := logFilePath(root)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer logFile.Close()

	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	cmd := exec.Command(exePath, "serve")
	cmd.Dir = root
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()

	fmt.Printf("serman-web started in background\n")
	fmt.Printf("pid: %d\n", cmd.Process.Pid)
	fmt.Printf("url: http://localhost:8080\n")
	fmt.Printf("log: %s\n", logPath)
	return nil
}

func stopBackgroundServer(root string) error {
	pidPath := pidFilePath(root)
	pid, ok := readRunningPID(pidPath)
	if !ok {
		_ = os.Remove(pidPath)
		fmt.Println("serman-web is not running")
		return nil
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}

	for i := 0; i < 20; i++ {
		if !isPIDRunning(pid) {
			_ = os.Remove(pidPath)
			fmt.Println("serman-web stopped")
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("process %d did not stop after SIGTERM", pid)
}

func printServerStatus(root string) error {
	pid, ok := readRunningPID(pidFilePath(root))
	if !ok {
		fmt.Println("serman-web status: stopped")
		return nil
	}

	fmt.Println("serman-web status: running")
	fmt.Printf("pid: %d\n", pid)
	fmt.Printf("url: http://localhost:8080\n")
	fmt.Printf("log: %s\n", logFilePath(root))
	return nil
}

func printHelp() {
	fmt.Print(helpText())
}

func helpText() string {
	return strings.TrimSpace(`
Usage:
  serman-web start    Start the web UI in the background
  serman-web stop     Stop the background web UI
  serman-web status   Show whether the web UI is running
  serman-web help     Show this help

Notes:
  - Logs are written to .serman/serman-web.log
  - PID is stored in .serman/serman-web.pid
  - Internal command: serman-web serve
`) + "\n"
}

func runtimeDir(root string) string {
	return filepath.Join(root, ".serman")
}

func pidFilePath(root string) string {
	return filepath.Join(runtimeDir(root), "serman-web.pid")
}

func logFilePath(root string) string {
	return filepath.Join(runtimeDir(root), "serman-web.log")
}

func ensureRuntimeDir(root string) error {
	return os.MkdirAll(runtimeDir(root), 0755)
}

func writePIDFile(path string, pid int) error {
	return os.WriteFile(path, []byte(strconv.Itoa(pid)), 0644)
}

func readRunningPID(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	if !isPIDRunning(pid) {
		return 0, false
	}
	return pid, true
}

func isPIDRunning(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
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
	return "", errors.New("docker-compose.yaml or services directory not found")
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
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

func openBrowser(url string) {
	time.Sleep(750 * time.Millisecond)

	launchers := [][]string{
		{"xdg-open", url},
		{"gio", "open", url},
		{"sensible-browser", url},
	}

	for _, args := range launchers {
		path, err := exec.LookPath(args[0])
		if err != nil {
			continue
		}

		cmd := exec.Command(path, args[1:]...)
		cmd.Stdin = nil
		cmd.Stdout = nil
		cmd.Stderr = nil
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err == nil {
			return
		}
	}

	fmt.Fprintf(os.Stderr, "warning: failed to open browser automatically, open %s manually\n", url)
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

func getComposeServices(root string) ([]ServiceStatus, error) {
	raw, err := composeOutput(root, "ps", "--format", "json")
	if err != nil {
		return nil, err
	}

	entries, err := parseComposePS(raw)
	if err != nil {
		return nil, err
	}
	known, err := composeServiceNames(root)
	if err != nil || len(known) == 0 {
		known, _ = composeFileServiceNames(root)
	}
	if len(known) == 0 {
		known = knownServiceNames()
	}

	statusByName := map[string]ServiceStatus{}
	for _, e := range entries {
		name := stringField(e, "Service")
		if name == "" {
			name = stringField(e, "Name")
		}
		if name == "" {
			continue
		}
		statusByName[name] = ServiceStatus{
			Name:   name,
			State:  pickState(e),
			Health: stringField(e, "Health"),
			Ports:  formatPorts(e),
		}
	}
	res := make([]ServiceStatus, 0, len(known))
	for _, name := range known {
		if st, ok := statusByName[name]; ok {
			res = append(res, st)
		} else {
			res = append(res, ServiceStatus{Name: name, State: "down"})
		}
	}
	sort.SliceStable(res, func(i, j int) bool {
		pi := serviceStatePriority(res[i].State)
		pj := serviceStatePriority(res[j].State)
		if pi != pj {
			return pi < pj
		}
		return res[i].Name < res[j].Name
	})
	return res, nil
}

func readServiceConfig(root, service string) (serviceConfigResponse, error) {
	cfg, ok := serman.ServiceByName(service)
	if !ok {
		return serviceConfigResponse{}, os.ErrNotExist
	}

	env, err := LoadEnvFile(filepath.Join(root, ".env"))
	if err != nil {
		return serviceConfigResponse{}, err
	}

	defaults := map[string]string{}
	if ex, err := LoadEnvFile(filepath.Join(root, ".env.example")); err == nil {
		for k, v := range ex.kv {
			defaults[k] = v
		}
	}

	fields := make([]serviceConfigField, 0, len(cfg.Env))
	for _, field := range cfg.Env {
		value, ok := env.Get(field.Key)
		if !ok {
			value = defaults[field.Key]
		}
		fields = append(fields, serviceConfigField{
			Key:   field.Key,
			Label: field.Label,
			Value: value,
		})
	}

	return serviceConfigResponse{Service: service, Fields: fields}, nil
}

func updateServiceConfig(root, service string, values map[string]string) error {
	cfg, ok := serman.ServiceByName(service)
	if !ok {
		return os.ErrNotExist
	}
	envPath := filepath.Join(root, ".env")
	env, err := LoadEnvFile(envPath)
	if err != nil {
		return err
	}
	for _, field := range cfg.Env {
		if value, ok := values[field.Key]; ok {
			env.Set(field.Key, value)
		}
	}
	return env.Save(envPath)
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

func unavailableServices(root string) []ServiceStatus {
	names, err := composeFileServiceNames(root)
	if err != nil {
		names = knownServiceNames()
	}
	res := make([]ServiceStatus, 0, len(names))
	for _, name := range names {
		res = append(res, ServiceStatus{Name: name, State: "unavailable"})
	}
	return res
}

func composeServiceNames(root string) ([]string, error) {
	out, err := composeOutput(root, "config", "--services")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	res := []string{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			res = append(res, line)
		}
	}
	return res, nil
}

func knownServiceNames() []string {
	names := make([]string, 0, len(serman.Services))
	for _, svc := range serman.Services {
		names = append(names, svc.Name)
	}
	sort.Strings(names)
	return names
}

func composeFileServiceNames(root string) ([]string, error) {
	paths, err := resolveComposeFiles(root)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var cfg struct {
			Services map[string]interface{} `yaml:"services"`
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, err
		}
		for name := range cfg.Services {
			seen[name] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil, errors.New("no services found in compose files")
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func ensureComposeFiles(root string) error {
	paths, err := resolveComposeFiles(root)
	if err != nil {
		return err
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("compose file missing: %s", filepath.Base(path))
			}
			return err
		}
	}
	return nil
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
	files := make([]string, 0)
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

func pickState(m map[string]interface{}) string {
	state := stringField(m, "State")
	if state == "" {
		state = stringField(m, "Status")
	}
	if state == "" {
		return "unknown"
	}
	return state
}

func serviceStatePriority(state string) int {
	s := strings.ToLower(strings.TrimSpace(state))
	switch {
	case strings.Contains(s, "running"):
		return 0
	case strings.Contains(s, "restarting"):
		return 1
	case s == "down":
		return 2
	default:
		return 3
	}
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
			parts = append(parts, fmt.Sprintf("%d→%d", pub, tgt))
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

func isAllowedAction(action string) bool {
	switch action {
	case "start", "stop", "restart", "fresh":
		return true
	default:
		return false
	}
}

func isKnownService(root, name string) bool {
	known, err := composeServiceNames(root)
	if err == nil {
		for _, n := range known {
			if n == name {
				return true
			}
		}
	}
	if known, err := composeFileServiceNames(root); err == nil {
		for _, n := range known {
			if n == name {
				return true
			}
		}
	}
	if _, ok := serman.ServiceByName(name); ok {
		return true
	}
	return false
}

func runAction(root, service, action string) error {
	switch action {
	case "start":
		return compose(root, "up", "-d", service)
	case "stop":
		return compose(root, "stop", service)
	case "restart":
		return compose(root, "restart", service)
	case "fresh":
		cfg, ok := serman.ServiceByName(service)
		if !ok {
			return freshService(root, service, nil)
		}
		return freshService(root, service, cfg.Volumes)
	default:
		return fmt.Errorf("unknown action: %s", action)
	}
}

func compose(root string, args ...string) error {
	_, err := composeOutput(root, args...)
	return err
}

func composeOutput(root string, args ...string) ([]byte, error) {
	composeFiles, err := resolveComposeFiles(root)
	if err != nil {
		return nil, err
	}
	envPath := filepath.Join(root, ".env")
	composeArgs := []string{"compose", "--project-directory", root}
	if _, err := os.Stat(envPath); err == nil {
		composeArgs = append(composeArgs, "--env-file", envPath)
	}
	for _, path := range composeFiles {
		composeArgs = append(composeArgs, "-f", path)
	}
	composeArgs = append(composeArgs, args...)
	cmd := exec.Command("docker", composeArgs...)
	cmd.Dir = root
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		return stdout.Bytes(), fmt.Errorf("docker compose %s: %w\n%s", strings.Join(args, " "), err, msg)
	}
	return stdout.Bytes(), nil
}

func freshService(root, service string, volumes []string) error {
	_ = compose(root, "stop", service)
	_ = compose(root, "rm", "-f", "-s", service)
	for _, v := range volumes {
		if err := removeVolume(v); err != nil {
			return err
		}
	}
	return compose(root, "up", "-d", service)
}

func removeVolume(name string) error {
	cmd := exec.Command("docker", "volume", "rm", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "No such volume") {
			return nil
		}
		return fmt.Errorf("docker volume rm %s: %w\n%s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func spaHandler(dir string) http.Handler {
	fs := http.Dir(dir)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "" {
			path = "/"
		}
		serveIndex := func() {
			f, err := fs.Open("/index.html")
			if err != nil {
				http.Error(w, "index.html not found", http.StatusNotFound)
				return
			}
			defer f.Close()
			if stat, err := f.Stat(); err == nil {
				http.ServeContent(w, r, "index.html", stat.ModTime(), f)
				return
			}
			io.Copy(w, f)
		}

		if path == "/" || strings.HasSuffix(path, "/") {
			serveIndex()
			return
		}

		f, err := fs.Open(path)
		if err != nil {
			serveIndex()
			return
		}
		defer f.Close()
		stat, err := f.Stat()
		if err != nil || stat.IsDir() {
			serveIndex()
			return
		}
		http.ServeContent(w, r, path, stat.ModTime(), f)
	})
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		fmt.Printf("%s %s %s\n", r.Method, r.URL.Path, time.Since(start).Truncate(time.Millisecond))
	})
}
