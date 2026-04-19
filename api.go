package main

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/emanspeaks/w84ggufman/internal/ini"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

type server struct {
	cfg       Config
	dl        *downloader
	preset    *presetManager
	vramBytes uint64
}

func newServer(cfg Config, dl *downloader, pm *presetManager) *server {
	vram := uint64(cfg.VramGiB * 1024 * 1024 * 1024)
	if vram == 0 {
		vram = detectVRAMBytes()
	}
	return &server{cfg: cfg, dl: dl, preset: pm, vramBytes: vram}
}

type diskInfo struct {
	TotalBytes uint64 `json:"totalBytes"`
	FreeBytes  uint64 `json:"freeBytes"`
	UsedBytes  uint64 `json:"usedBytes"`
}

func getDiskInfo(path string) (diskInfo, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return diskInfo{}, err
	}
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bavail * uint64(stat.Bsize)
	return diskInfo{
		TotalBytes: total,
		FreeBytes:  free,
		UsedBytes:  total - free,
	}, nil
}

type localModel struct {
	Name        string            `json:"name"`
	Path        string            `json:"path"`
	SizeBytes   int64             `json:"sizeBytes"`
	Files       []string          `json:"files"`
	Loaded      bool              `json:"loaded"`
	IsVision    bool              `json:"isVision"`
	Mmproj      string            `json:"mmproj"`
	InPreset    bool              `json:"inPreset"`
	PresetEntry map[string]string `json:"presetEntry,omitempty"`
	RepoID      string            `json:"repoId,omitempty"`
}

func (s *server) handleLocal(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(s.cfg.ModelsDir)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, []localModel{})
			return
		}
		http.Error(w, "failed to read models dir: "+err.Error(), http.StatusInternalServerError)
		return
	}

	loadedModels, _ := s.fetchLoadedModels()
	var presetFile *ini.File
	presetFile, _ = s.preset.Load()

	models := make([]localModel, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		parentDir := filepath.Join(s.cfg.ModelsDir, entry.Name())
		subEntries, err := os.ReadDir(parentDir)
		if err != nil {
			continue
		}

		// Find mmproj and model files at the parent (top) level.
		var topModelFiles []string
		var topMmprojFile string
		for _, sub := range subEntries {
			if sub.IsDir() || !strings.HasSuffix(sub.Name(), ".gguf") {
				continue
			}
			if matchesMmproj(sub.Name()) {
				if topMmprojFile == "" {
					topMmprojFile = sub.Name()
				}
			} else {
				topModelFiles = append(topModelFiles, sub.Name())
			}
		}

		// Check for quant subdirectories (new nested layout).
		var quantSubdirs []fs.DirEntry
		for _, sub := range subEntries {
			if !sub.IsDir() {
				continue
			}
			quantDir := filepath.Join(parentDir, sub.Name())
			subFiles, _ := os.ReadDir(quantDir)
			for _, f := range subFiles {
				if !f.IsDir() && strings.HasSuffix(f.Name(), ".gguf") && !matchesMmproj(f.Name()) {
					quantSubdirs = append(quantSubdirs, sub)
					break
				}
			}
		}

		if len(quantSubdirs) > 0 {
			// New nested layout: one card per quant subdir, mmproj shared from parent.
			repoID := readModelMeta(parentDir).RepoID
			for _, sub := range quantSubdirs {
				quantDir := filepath.Join(parentDir, sub.Name())
				var quantFiles []string
				var totalSize int64
				filepath.WalkDir(quantDir, func(path string, d fs.DirEntry, err error) error {
					if err != nil || d.IsDir() {
						return nil
					}
					if strings.HasSuffix(d.Name(), ".gguf") && !matchesMmproj(d.Name()) {
						if info, e := d.Info(); e == nil {
							totalSize += info.Size()
						}
						quantFiles = append(quantFiles, d.Name())
					}
					return nil
				})
				if len(quantFiles) == 0 {
					continue
				}

				name := sub.Name()
				_, loaded := loadedModels[name]
				inPreset := false
				var presetEntry map[string]string
				if presetFile != nil {
					if sec, ok := presetFile.Sections[name]; ok {
						inPreset = true
						presetEntry = sec
					}
				}
				if repoID == "" {
					repoID = detectRepoIDFromGGUF(quantDir, quantFiles)
					if repoID != "" {
						_ = writeModelMeta(parentDir, repoID)
					}
				}
				models = append(models, localModel{
					Name:        name,
					Path:        quantDir,
					SizeBytes:   totalSize,
					Files:       quantFiles,
					Loaded:      loaded,
					IsVision:    topMmprojFile != "",
					Mmproj:      topMmprojFile,
					InPreset:    inPreset,
					PresetEntry: presetEntry,
					RepoID:      repoID,
				})
			}
		} else if len(topModelFiles) > 0 {
			// Old flat layout: model files sit directly in this directory.
			var totalSize int64
			for _, f := range topModelFiles {
				if info, err := os.Stat(filepath.Join(parentDir, f)); err == nil {
					totalSize += info.Size()
				}
			}
			name := entry.Name()
			_, loaded := loadedModels[name]
			inPreset := false
			var presetEntry map[string]string
			if presetFile != nil {
				if sec, ok := presetFile.Sections[name]; ok {
					inPreset = true
					presetEntry = sec
				}
			}
			repoID := readModelMeta(parentDir).RepoID
			if repoID == "" {
				repoID = detectRepoIDFromGGUF(parentDir, topModelFiles)
				if repoID != "" {
					_ = writeModelMeta(parentDir, repoID)
				}
			}
			models = append(models, localModel{
				Name:        name,
				Path:        parentDir,
				SizeBytes:   totalSize,
				Files:       topModelFiles,
				Loaded:      loaded,
				IsVision:    topMmprojFile != "",
				Mmproj:      topMmprojFile,
				InPreset:    inPreset,
				PresetEntry: presetEntry,
				RepoID:      repoID,
			})
		}
	}
	writeJSON(w, models)
}

// findQuantDir searches for a model dir with the given name.
// It first checks nested quant subdirs (new layout: ModelsDir/parent/name),
// then falls back to top-level dirs (old layout: ModelsDir/name).
// Returns the model dir, its parent dir, and whether it's nested.
func (s *server) findQuantDir(name string) (modelDir, parentDir string, nested bool) {
	if parents, err := os.ReadDir(s.cfg.ModelsDir); err == nil {
		for _, p := range parents {
			if !p.IsDir() {
				continue
			}
			pDir := filepath.Join(s.cfg.ModelsDir, p.Name())
			candidate := filepath.Join(pDir, name)
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				return candidate, pDir, true
			}
		}
	}
	topLevel := filepath.Join(s.cfg.ModelsDir, name)
	return topLevel, s.cfg.ModelsDir, false
}

type llamaModelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

func (s *server) fetchLoadedModels() (map[string]struct{}, error) {
	resp, err := http.Get(s.cfg.LlamaServerURL + "/v1/models")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var lmr llamaModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&lmr); err != nil {
		return nil, err
	}
	loaded := make(map[string]struct{}, len(lmr.Data))
	for _, m := range lmr.Data {
		loaded[m.ID] = struct{}{}
	}
	return loaded, nil
}

func (s *server) handleDeleteLocal(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
		http.Error(w, "invalid model name", http.StatusBadRequest)
		return
	}
	modelDir, parentDir, nested := s.findQuantDir(name)
	if _, err := os.Stat(modelDir); os.IsNotExist(err) {
		http.Error(w, "model not found", http.StatusNotFound)
		return
	}
	if err := removeAllWritable(modelDir); err != nil {
		log.Printf("error: delete model %q: %v", name, err)
		http.Error(w, "failed to delete: "+err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("deleted model %q", name)

	// If nested, remove the parent dir when no more quant subdirs remain.
	if nested {
		remaining, _ := os.ReadDir(parentDir)
		hasQuants := false
		for _, e := range remaining {
			if !e.IsDir() {
				continue
			}
			subFiles, _ := os.ReadDir(filepath.Join(parentDir, e.Name()))
			for _, f := range subFiles {
				if !f.IsDir() && strings.HasSuffix(f.Name(), ".gguf") && !matchesMmproj(f.Name()) {
					hasQuants = true
					break
				}
			}
			if hasQuants {
				break
			}
		}
		if !hasQuants {
			if err := removeAllWritable(parentDir); err != nil {
				log.Printf("warning: could not remove empty parent dir %q: %v", parentDir, err)
			}
		}
	}

	if err := s.preset.RemoveModel(name); err != nil {
		log.Printf("warning: failed to remove %s from managed.ini: %v", name, err)
	}
	if err := restartService(s.cfg.LlamaService); err != nil {
		log.Printf("warning: failed to restart %s: %v", s.cfg.LlamaService, err)
	}
	w.WriteHeader(http.StatusNoContent)
}

type statusResponse struct {
	LlamaReachable     bool     `json:"llamaReachable"`
	DownloadInProgress bool     `json:"downloadInProgress"`
	ActiveDownload     string   `json:"activeDownload"`
	Version            string   `json:"version"`
	Disk               diskInfo `json:"disk"`
	WarnDownloadBytes  uint64   `json:"warnDownloadBytes"`
	VramBytes          uint64   `json:"vramBytes"`
	VramUsedBytes      uint64   `json:"vramUsedBytes"`
	VramUsedKnown      bool     `json:"vramUsedKnown"`
	WarnVramBytes      uint64   `json:"warnVramBytes"`
	LoadedModels       []string `json:"loadedModels"`
}

func (s *server) handleStatus(w http.ResponseWriter, r *http.Request) {
	reachable := true
	var loadedIDs []string
	resp, err := http.Get(s.cfg.LlamaServerURL + "/v1/models")
	if err != nil {
		reachable = false
	} else {
		var lmr llamaModelsResponse
		if err := json.NewDecoder(resp.Body).Decode(&lmr); err == nil {
			for _, m := range lmr.Data {
				loadedIDs = append(loadedIDs, m.ID)
			}
		}
		resp.Body.Close()
	}
	active, inProgress := s.dl.activeInfo()
	disk, _ := getDiskInfo(s.cfg.ModelsDir)
	warnBytes := uint64(s.cfg.WarnDownloadGiB * 1024 * 1024 * 1024)
	pct := s.cfg.WarnVramPercent
	if pct <= 0 {
		pct = 80
	}
	warnVram := uint64(float64(s.vramBytes) * pct / 100)
	vramUsed, vramUsedKnown := detectVRAMUsedBytes()
	writeJSON(w, statusResponse{
		LlamaReachable:     reachable,
		DownloadInProgress: inProgress,
		ActiveDownload:     active,
		Version:            version,
		Disk:               disk,
		WarnDownloadBytes:  warnBytes,
		VramBytes:          s.vramBytes,
		VramUsedBytes:      vramUsed,
		VramUsedKnown:      vramUsedKnown,
		WarnVramBytes:      warnVram,
		LoadedModels:       loadedIDs,
	})
}

var mdRenderer = goldmark.New(goldmark.WithExtensions(extension.GFM))

func (s *server) handleReadme(w http.ResponseWriter, r *http.Request) {
	repoID := r.URL.Query().Get("id")
	if repoID == "" {
		http.Error(w, "missing id parameter", http.StatusBadRequest)
		return
	}
	if strings.Count(repoID, "/") != 1 || strings.Contains(repoID, "..") || strings.ContainsAny(repoID, " \t\n") {
		http.Error(w, "invalid repo id", http.StatusBadRequest)
		return
	}
	url := "https://huggingface.co/" + repoID + "/resolve/main/README.md"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if s.cfg.HFToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.HFToken)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "failed to fetch readme: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if resp.StatusCode != http.StatusOK {
		http.Error(w, "HuggingFace returned non-OK status", http.StatusBadGateway)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		http.Error(w, "failed to read readme", http.StatusInternalServerError)
		return
	}
	raw = stripFrontmatter(raw)
	var buf bytes.Buffer
	if err := mdRenderer.Convert(raw, &buf); err != nil {
		http.Error(w, "failed to render readme", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(buf.Bytes())
}

// stripFrontmatter removes the YAML ---...--- block at the top of model cards.
func stripFrontmatter(b []byte) []byte {
	if !bytes.HasPrefix(b, []byte("---")) {
		return b
	}
	end := bytes.Index(b[3:], []byte("\n---"))
	if end < 0 {
		return b
	}
	rest := b[3+end+4:]
	return bytes.TrimLeft(rest, "\n")
}

func (s *server) handleRepo(w http.ResponseWriter, r *http.Request) {
	repoID := r.URL.Query().Get("id")
	if repoID == "" {
		http.Error(w, "missing id parameter", http.StatusBadRequest)
		return
	}
	info, err := fetchRepoInfo(repoID, s.cfg.HFToken)
	if err != nil {
		http.Error(w, "failed to fetch repo: "+err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, info)
}

func (s *server) handleDownload(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RepoID       string   `json:"repoId"`
		Filenames    []string `json:"filenames"`
		SidecarFiles []string `json:"sidecarFiles"`
		TotalBytes   int64    `json:"totalBytes"`
		Force        bool     `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.RepoID == "" || len(req.Filenames) == 0 {
		http.Error(w, "repoId and filenames are required", http.StatusBadRequest)
		return
	}

	if !req.Force {
		// Check each quant's destination directory individually (new nested layout:
		// ModelsDir/basename(repoID)/quantName).
		parentDir := filepath.Join(s.cfg.ModelsDir, filepath.Base(req.RepoID))
		for _, filename := range req.Filenames {
			name := modelNameFromFilename(filename)
			if name == "" {
				continue
			}
			destDir := filepath.Join(parentDir, name)
			if _, err := os.Stat(destDir); err == nil {
				existingRepoID := readModelMeta(parentDir).RepoID
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(map[string]string{
					"conflict":       "exists",
					"modelName":      name,
					"existingRepoId": existingRepoID,
				})
				return
			}
		}
	}

	if err := s.dl.start(req.RepoID, req.Filenames, req.SidecarFiles, req.TotalBytes, req.Force); err != nil {
		log.Printf("error: start download %s %v: %v", req.RepoID, req.Filenames, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"conflict": "busy", "message": err.Error()})
		return
	}
	log.Printf("download queued: %s %v", req.RepoID, req.Filenames)
	w.WriteHeader(http.StatusAccepted)
}

func (s *server) handleCancelDownload(w http.ResponseWriter, r *http.Request) {
	s.dl.cancelDownload()
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleDownloadStatus(w http.ResponseWriter, r *http.Request) {
	s.dl.streamSSE(w, r)
}

func (s *server) handleRestart(w http.ResponseWriter, r *http.Request) {
	log.Printf("restarting service %s", s.cfg.LlamaService)
	if err := restartService(s.cfg.LlamaService); err != nil {
		log.Printf("error: restart %s: %v", s.cfg.LlamaService, err)
		http.Error(w, "failed to restart service: "+err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("service %s restarted", s.cfg.LlamaService)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleGetPreset(w http.ResponseWriter, r *http.Request) {
	f, err := s.preset.Load()
	if err != nil {
		http.Error(w, "failed to load preset: "+err.Error(), http.StatusInternalServerError)
		return
	}
	type presetResponse struct {
		Global   map[string]string            `json:"global"`
		Sections map[string]map[string]string `json:"sections"`
	}
	writeJSON(w, presetResponse{Global: f.Global, Sections: f.Sections})
}

func (s *server) handleUpdatePresetGlobal(w http.ResponseWriter, r *http.Request) {
	var kvs map[string]string
	if err := json.NewDecoder(r.Body).Decode(&kvs); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := s.preset.UpdateGlobal(kvs); err != nil {
		http.Error(w, "failed to update preset: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleUpdatePresetModel(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
		http.Error(w, "invalid model name", http.StatusBadRequest)
		return
	}
	var kvs map[string]string
	if err := json.NewDecoder(r.Body).Decode(&kvs); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := s.preset.UpsertModelKeys(name, kvs); err != nil {
		http.Error(w, "failed to update preset: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleGetPresetRaw(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
		http.Error(w, "invalid model name", http.StatusBadRequest)
		return
	}
	body, err := s.preset.ReadRaw(name)
	if err != nil {
		http.Error(w, "failed to read preset: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(body))
}

func (s *server) handleUpdatePresetRaw(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
		http.Error(w, "invalid model name", http.StatusBadRequest)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		http.Error(w, "failed to read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	body := strings.TrimRight(string(raw), "\r\n")
	if err := s.preset.WriteRaw(name, body); err != nil {
		http.Error(w, "failed to write preset: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleRestartSelf(w http.ResponseWriter, r *http.Request) {
	if s.cfg.SelfService == "" {
		http.Error(w, "selfService not configured", http.StatusNotImplemented)
		return
	}
	log.Printf("restarting self service %s", s.cfg.SelfService)
	w.WriteHeader(http.StatusAccepted)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go func() {
		time.Sleep(500 * time.Millisecond)
		if err := restartService(s.cfg.SelfService); err != nil {
			log.Printf("error: restart self %s: %v", s.cfg.SelfService, err)
		}
	}()
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
