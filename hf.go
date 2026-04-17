package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type HFFile struct {
	Filename    string `json:"filename"`
	Size        *int64 `json:"size"`
	DownloadURL string `json:"downloadURL"`
}

type hfModelResponse struct {
	Siblings []struct {
		Rfilename string `json:"rfilename"`
		Size      *int64 `json:"size"`
	} `json:"siblings"`
}

func fetchRepoFiles(repoID, token string) ([]HFFile, error) {
	req, err := http.NewRequest("GET", "https://huggingface.co/api/models/"+repoID, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HuggingFace API returned %d", resp.StatusCode)
	}
	var model hfModelResponse
	if err := json.NewDecoder(resp.Body).Decode(&model); err != nil {
		return nil, err
	}
	files := make([]HFFile, 0)
	for _, s := range model.Siblings {
		if !strings.HasSuffix(s.Rfilename, ".gguf") {
			continue
		}
		files = append(files, HFFile{
			Filename:    s.Rfilename,
			Size:        s.Size,
			DownloadURL: "https://huggingface.co/" + repoID + "/resolve/main/" + s.Rfilename,
		})
	}
	return files, nil
}
