package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/InazumaV/V2bX/common/json5"
	"github.com/goccy/go-json"
	"github.com/spf13/cobra"
)

var (
	splitConfigPath string
	splitOutDir     string
	splitForce      bool
)

var splitCommand = cobra.Command{
	Use:   "split",
	Short: "Write one config file per node (one process per node)",
	Long: `Split a multi node config into one config file per node.

Every node runs in its own process, which is the only way to fully isolate
nodes: the DNS config, the CPU and the bandwidth of one node can no longer
affect another one, and a node that fails to start cannot take the others down.

The generated files are written next to each other (default /etc/V2bX/nodes) and
are meant to be started with a systemd template unit:

  V2bX split -c /etc/V2bX/config.json -o /etc/V2bX/nodes
  systemctl stop V2bX && systemctl disable V2bX
  systemctl enable --now v2bx@<node name>

"v2bx" (the menu script) can do the migration and the rollback for you.

The source config is never modified: rolling back is starting V2bX again.`,
	Run:  splitHandle,
	Args: cobra.NoArgs,
}

func init() {
	splitCommand.PersistentFlags().
		StringVarP(&splitConfigPath, "config", "c",
			"/etc/V2bX/config.json", "config file path")
	splitCommand.PersistentFlags().
		StringVarP(&splitOutDir, "out", "o",
			"/etc/V2bX/nodes", "directory the per node configs are written to")
	splitCommand.PersistentFlags().
		BoolVarP(&splitForce, "force", "f",
			false, "overwrite existing config and DNS files")
	command.AddCommand(&splitCommand)
}

// dnsCopy is a DNS config file that has to exist for one generated config.
type dnsCopy struct {
	// Source is the file the current (multi node) config points to.
	Source string
	// Target is the per node copy that the generated config points to.
	Target string
}

// splitFile is one generated per node config.
type splitFile struct {
	// Name is the node name the file is named after: the file is
	// <outDir>/<Name>.json and the systemd instance is v2bx@<Name>.service.
	Name string
	// Path is the generated config file.
	Path string
	// Content is the config file itself.
	Content []byte
	// Dns lists the DNS files that must be copied for this node.
	Dns []dnsCopy
}

// buildSplitConfigs builds one config per node from the raw JSON of a multi node
// config.
//
// It works on the raw JSON instead of conf.Conf on purpose: a node entry is
// parsed into flat ApiConfig/Options structs, so marshalling it back would drop
// every key this version does not know and the Include form.
//
// DnsConfigPath is rewritten to a per node file. updateDNSConfig writes that
// file whenever a node reloads, and conf.Watch restarts the process when the
// file changes, so a shared DNS file would make every other node restart as
// well. Log.Output is rewritten too: lumberjack has no cross process locking,
// several processes rotating one file lose lines.
func buildSplitConfigs(raw []byte, outDir string) ([]splitFile, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("decode config error: %s", err)
	}

	var nodes []map[string]json.RawMessage
	if rawNodes, ok := doc["Nodes"]; ok {
		if err := json.Unmarshal(rawNodes, &nodes); err != nil {
			return nil, fmt.Errorf("decode nodes error: %s", err)
		}
	}
	if len(nodes) == 0 {
		return nil, errors.New("the config has no nodes")
	}

	var cores []map[string]json.RawMessage
	if rawCores, ok := doc["Cores"]; ok {
		if err := json.Unmarshal(rawCores, &cores); err != nil {
			return nil, fmt.Errorf("decode cores error: %s", err)
		}
	}

	used := make(map[string]int, len(nodes))
	files := make([]splitFile, 0, len(nodes))
	for i, node := range nodes {
		name := uniqueSplitName(used, splitNodeName(node))

		perCores := make([]map[string]json.RawMessage, 0, len(cores))
		var copies []dnsCopy
		for ci, core := range cores {
			copied := make(map[string]json.RawMessage, len(core))
			for k, v := range core {
				copied[k] = v
			}
			if current, ok := core["DnsConfigPath"]; ok {
				var path string
				if err := json.Unmarshal(current, &path); err != nil {
					return nil, fmt.Errorf("decode Cores[%d].DnsConfigPath error: %s", ci, err)
				}
				if path != "" {
					target := filepath.Join(outDir, "dns", splitDnsFileName(name, core))
					if encoded, err := json.Marshal(target); err == nil {
						copied["DnsConfigPath"] = encoded
					}
					copies = append(copies, dnsCopy{Source: path, Target: target})
				}
			}
			perCores = append(perCores, copied)
		}

		out := make(map[string]json.RawMessage, 3)
		if logRaw, ok := doc["Log"]; ok {
			if rewritten, err := rewriteLogOutput(logRaw, outDir, name); err == nil {
				out["Log"] = rewritten
			} else {
				return nil, err
			}
		}
		if encoded, err := json.Marshal(perCores); err == nil {
			out["Cores"] = encoded
		}
		if encoded, err := json.Marshal([]map[string]json.RawMessage{node}); err == nil {
			out["Nodes"] = encoded
		}
		content, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshal node %d error: %s", i, err)
		}
		content = append(content, '\n')

		files = append(files, splitFile{
			Name:    name,
			Path:    filepath.Join(outDir, name+".json"),
			Content: content,
			Dns:     copies,
		})
	}

	return files, nil
}

// rewriteLogOutput points the per node config at its own log file. An empty
// Output (the journal) is kept as is.
func rewriteLogOutput(logRaw json.RawMessage, outDir, name string) (json.RawMessage, error) {
	var logConfig map[string]json.RawMessage
	if err := json.Unmarshal(logRaw, &logConfig); err != nil {
		return nil, fmt.Errorf("decode Log error: %s", err)
	}
	if current, ok := logConfig["Output"]; ok {
		var path string
		if err := json.Unmarshal(current, &path); err != nil {
			return nil, fmt.Errorf("decode Log.Output error: %s", err)
		}
		if path != "" {
			target := filepath.Join(outDir, "log", splitLogFileName(name, path))
			if encoded, err := json.Marshal(target); err == nil {
				logConfig["Output"] = encoded
			}
		}
	}
	encoded, err := json.Marshal(logConfig)
	if err != nil {
		return nil, fmt.Errorf("encode Log error: %s", err)
	}

	return encoded, nil
}

// splitLogFileName keeps the file name and the extension of the configured log
// file and inserts the node name: /var/log/V2bX.log becomes V2bX-<name>.log.
func splitLogFileName(name, path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext) + "-" + name + ext
}

// splitDnsFileName names the per node DNS file. A core with an explicit Name is
// part of the name, so several cores of one node cannot end up with one file
// (their DNS formats differ). The files are written into the dns subdirectory of
// outDir: the files directly in outDir are exactly the configs of the instances.
func splitDnsFileName(name string, core map[string]json.RawMessage) string {
	base := "dns_" + name
	if raw, ok := core["Name"]; ok {
		var coreName string
		if err := json.Unmarshal(raw, &coreName); err == nil && coreName != "" {
			base += "_" + sanitizeSplitName(coreName)
		}
	}

	return base + ".json"
}

// splitNodeName is the name of the generated file: the configured Name, or the
// same tag the controller builds when Name is empty.
func splitNodeName(node map[string]json.RawMessage) string {
	var name string
	if raw, ok := node["Name"]; ok {
		_ = json.Unmarshal(raw, &name)
	}
	if name = strings.TrimSpace(name); name != "" {
		return sanitizeSplitName(name)
	}

	var apiHost, nodeType string
	var nodeID int
	_ = json.Unmarshal(node["ApiHost"], &apiHost)
	_ = json.Unmarshal(node["NodeType"], &nodeType)
	_ = json.Unmarshal(node["NodeID"], &nodeID)
	host := strings.TrimPrefix(strings.TrimPrefix(apiHost, "https://"), "http://")
	host = strings.TrimSuffix(host, "/")
	host = strings.ReplaceAll(host, ":", "_")
	derived := fmt.Sprintf("%s-%s-%d", host, nodeType, nodeID)

	return sanitizeSplitName(derived)
}

// sanitizeSplitName keeps only the characters a systemd instance name and a
// file name can carry.
func sanitizeSplitName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "._-")
	if out == "" {
		return "node"
	}

	return out
}

// uniqueSplitName makes the generated names unique, so an empty or duplicated
// Name cannot make two nodes write the same file.
func uniqueSplitName(used map[string]int, name string) string {
	used[name]++
	if n := used[name]; n > 1 {
		return fmt.Sprintf("%s-%d", name, n)
	}

	return name
}

// readSplitConfig reads a config file, tolerating the comments JSON5 allows.
func readSplitConfig(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s error: %s", path, err)
	}
	defer f.Close()
	raw, err := io.ReadAll(json5.NewTrimNodeReader(f))
	if err != nil {
		return nil, fmt.Errorf("read %s error: %s", path, err)
	}

	return raw, nil
}

// runSplit writes one config per node next to each other.
func runSplit(configPath, outDir string, force bool) ([]splitFile, error) {
	raw, err := readSplitConfig(configPath)
	if err != nil {
		return nil, err
	}

	files, err := buildSplitConfigs(raw, outDir)
	if err != nil {
		return nil, fmt.Errorf("split %s error: %s", configPath, err)
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("create %s error: %s", outDir, err)
	}
	// The per node log files live in a subdirectory, so the directory has to
	// exist before the first instance starts writing (lumberjack creates it on
	// demand, but a missing directory would be an error the admin has to read in
	// the journal of a node that is already down). The DNS copies live in their
	// own subdirectory as well, so the configs of the instances are the only
	// *.json files in outDir and the menu can list them without guessing.
	for _, sub := range []string{"log", "dns"} {
		if err := os.MkdirAll(filepath.Join(outDir, sub), 0o755); err != nil {
			return nil, fmt.Errorf("create %s error: %s", filepath.Join(outDir, sub), err)
		}
	}

	configDir := filepath.Dir(configPath)
	for _, file := range files {
		if !force {
			if _, err := os.Stat(file.Path); err == nil {
				return nil, fmt.Errorf("%s exists, use -f to overwrite", file.Path)
			}
		}
		for _, dns := range file.Dns {
			sourcePath := dns.Source
			if !filepath.IsAbs(sourcePath) {
				sourcePath = filepath.Join(configDir, sourcePath)
			}
			if err := copySplitFile(sourcePath, dns.Target, force); err != nil {
				return nil, fmt.Errorf("copy DNS config error: %s", err)
			}
		}
		if err := os.WriteFile(file.Path, file.Content, 0o600); err != nil {
			return nil, fmt.Errorf("write %s error: %s", file.Path, err)
		}
	}

	return files, nil
}

func splitHandle(_ *cobra.Command, _ []string) {
	files, err := runSplit(splitConfigPath, splitOutDir, splitForce)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	for _, file := range files {
		fmt.Printf("%s -> %s\n", file.Name, file.Path)
	}

	fmt.Println()
	fmt.Println("Start one process per node with the systemd template unit:")
	fmt.Println("  systemctl stop V2bX && systemctl disable V2bX")
	for _, file := range files {
		fmt.Printf("  systemctl enable --now v2bx@%s\n", file.Name)
	}
	fmt.Println()
	fmt.Printf("%s was not modified: `systemctl enable --now V2bX` switches back.\n", splitConfigPath)
}

// copySplitFile copies the DNS config next to the generated configs. A missing
// source is not fatal: a node whose panel sends no DNS never writes the file,
// and xray builds its default DNS when the file is absent.
func copySplitFile(source, target string, force bool) error {
	if !force {
		if _, err := os.Stat(target); err == nil {
			return nil
		}
	}
	content, err := os.ReadFile(source)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "warning: %s does not exist, writing a default DNS config\n", source)
			content = []byte("{\n  \"servers\": [\n    \"1.1.1.1\",\n    \"localhost\"\n  ],\n  \"tag\": \"dns_inbound\"\n}\n")
		} else {
			return err
		}
	}

	return os.WriteFile(target, content, 0o644)
}
