package api

import (
	"io/fs"
	"net/http"
	"path/filepath"
)

type diskUsageFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

type diskUsageResponse struct {
	TotalBytes     uint64          `json:"totalBytes"`
	FreeBytes      uint64          `json:"freeBytes"`
	UsedBytes      uint64          `json:"usedBytes"`
	ModelsDir      string          `json:"modelsDir"`
	ModelsDirBytes uint64          `json:"modelsDirBytes"`
	SystemBytes    uint64          `json:"systemBytes"`
	Files          []diskUsageFile `json:"files"`
}

func (s *Server) HandleDiskUsage(w http.ResponseWriter, r *http.Request) {
	disk, _ := getDiskInfo(s.cfg.ModelsDir)
	files := []diskUsageFile{}
	var modelsTotal uint64
	filepath.WalkDir(s.cfg.ModelsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		rel, rerr := filepath.Rel(s.cfg.ModelsDir, path)
		if rerr != nil {
			rel = path
		}
		size := info.Size()
		files = append(files, diskUsageFile{Path: filepath.ToSlash(rel), Size: size})
		if size > 0 {
			modelsTotal += uint64(size)
		}
		return nil
	})
	var system uint64
	if disk.UsedBytes > modelsTotal {
		system = disk.UsedBytes - modelsTotal
	}
	writeJSON(w, diskUsageResponse{
		TotalBytes:     disk.TotalBytes,
		FreeBytes:      disk.FreeBytes,
		UsedBytes:      disk.UsedBytes,
		ModelsDir:      s.cfg.ModelsDir,
		ModelsDirBytes: modelsTotal,
		SystemBytes:    system,
		Files:          files,
	})
}
