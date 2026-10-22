// SPDX-License-Identifier: MIT

package configfile

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/musakkka/5050cloud/pkg/manifest"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// ParseNixpacks reads nixpacks.toml (9.20.2). Keys we map are kept; every
// other top-level table or key is listed in IgnoredKeys.
func ParseNixpacks(name string, data []byte) (*manifest.NixpacksHints, error) {
	if len(data) > MaxFileBytes {
		return nil, &Error{Path: name, Msg: "the file is larger than 1 MB."}
	}
	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		line := 0
		var pe toml.ParseError
		if asTOMLParseError(err, &pe) {
			line = pe.Position.Line
		}
		return nil, &Error{Path: name, Line: line, Msg: "the TOML is not valid: " + firstLine(err.Error()) + "."}
	}
	lines := tomlLines(data)
	h := &manifest.NixpacksHints{SourceLines: map[string]int{}}
	var ignored []string
	for k, v := range raw {
		switch k {
		case "start":
			m, _ := v.(map[string]any)
			for sk, sv := range m {
				if sk == "cmd" {
					h.StartCmd, _ = sv.(string)
					h.SourceLines["startCmd"] = lines["start.cmd"]
				} else {
					ignored = append(ignored, "start."+sk)
				}
			}
		case "variables":
			m, _ := v.(map[string]any)
			if len(m) > 0 {
				h.Variables = map[string]string{}
				for vk, vv := range m {
					h.Variables[vk] = fmt.Sprint(vv)
				}
				h.SourceLines["variables"] = lines["variables"]
			}
		case "phases":
			m, _ := v.(map[string]any)
			for phase, pv := range m {
				pm, _ := pv.(map[string]any)
				switch phase {
				case "build":
					for pk, val := range pm {
						if pk == "cmds" {
							h.BuildCmds = toStrings(val)
							h.SourceLines["buildCmds"] = lines["phases.build.cmds"]
						} else {
							ignored = append(ignored, "phases.build."+pk)
						}
					}
				case "setup":
					for pk, val := range pm {
						switch pk {
						case "nixPkgs", "nixpkgs":
							h.NixPkgs = toStrings(val)
							h.SourceLines["nixPkgs"] = lines["phases.setup."+pk]
						case "aptPkgs", "aptpkgs":
							h.AptPkgs = toStrings(val)
							h.SourceLines["aptPkgs"] = lines["phases.setup."+pk]
						default:
							ignored = append(ignored, "phases.setup."+pk)
						}
					}
				default:
					ignored = append(ignored, "phases."+phase)
				}
			}
		case "providers":
			if p := toStrings(v); len(p) > 0 {
				h.ProviderHint = p[0]
			}
			ignored = append(ignored, "providers")
		default:
			ignored = append(ignored, k)
		}
	}
	sort.Strings(ignored)
	h.IgnoredKeys = ignored
	for k, l := range h.SourceLines {
		if l == 0 {
			delete(h.SourceLines, k)
		}
	}
	if len(h.SourceLines) == 0 {
		h.SourceLines = nil
	}
	return h, nil
}

func toStrings(v any) []string {
	switch x := v.(type) {
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return x
	case string:
		return []string{x}
	}
	return nil
}

// Find returns the config file for a service: configPath (relative to the
// repository root) when set, else the first of Candidates in rootDir. It
// returns "" when there is none.
func Find(fsys fs.FS, rootDir, configPath string) (string, error) {
	if configPath != "" {
		p := clean(configPath)
		if _, err := fs.Stat(fsys, p); err != nil {
			return "", &Error{Path: configPath, Msg: "the config file path set in Settings > Source does not exist in the repository."}
		}
		return p, nil
	}
	dir := clean(rootDir)
	for _, c := range Candidates {
		p := path.Join(dir, c)
		if st, err := fs.Stat(fsys, p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", nil
}

func clean(p string) string {
	p = strings.TrimPrefix(path.Clean("/"+p), "/")
	if p == "" {
		return "."
	}
	return p
}

// Load finds and parses the service's config file and nixpacks.toml from
// a checked-out source tree. It returns nil when neither exists.
func Load(fsys fs.FS, rootDir, configPath string) (*proto.ConfigOverlay, error) {
	p, err := Find(fsys, rootDir, configPath)
	if err != nil {
		return nil, err
	}
	var f *File
	if p != "" {
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil, &Error{Path: p, Msg: "the file cannot be read: " + err.Error() + "."}
		}
		if f, err = Parse(p, data); err != nil {
			return nil, err
		}
	}
	nixName := "nixpacks.toml"
	if f != nil && f.Config.Build != nil && f.Config.Build.NixpacksConfigPath != nil {
		nixName = *f.Config.Build.NixpacksConfigPath
	}
	var nix *manifest.NixpacksHints
	np := path.Join(clean(rootDir), clean(nixName))
	if data, err := fs.ReadFile(fsys, np); err == nil {
		if nix, err = ParseNixpacks(np, data); err != nil {
			return nil, err
		}
	}
	return Overlay(f, nix), nil
}
