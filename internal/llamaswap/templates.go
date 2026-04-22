package llamaswap

import (
	"strconv"
	"strings"
)

// Placeholder tokens substituted in body templates when generating a model entry.
const (
	PlaceholderModelPath  = "{{MODEL_PATH}}"
	PlaceholderModelName  = "{{MODEL_NAME}}"
	PlaceholderMmprojLine = "{{MMPROJ_LINE}}"
	PlaceholderVaeLine    = "{{VAE_LINE}}"
)

// DefaultLLMBody is the built-in LLM model entry body at 0-indent.
// ttl: -1 is expanded to the size-based heuristic by ApplyBodyTemplate.
const DefaultLLMBody = "cmd: |\n" +
	"  ${llama-command-template}\n" +
	"    -c ${default-context}\n" +
	"    -m {{MODEL_PATH}}\n" +
	"    {{MMPROJ_LINE}}\n" +
	"    --alias {{MODEL_NAME}}\n" +
	"ttl: -1\n" +
	"metadata:\n" +
	"  model_type: llm\n" +
	"  port: ${PORT}"

// DefaultSDBody is the built-in SD model entry body at 0-indent.
const DefaultSDBody = "cmd: |\n" +
	"  ${sd-command-template}\n" +
	"    --diffusion-model {{MODEL_PATH}}\n" +
	"    {{VAE_LINE}}\n" +
	"ttl: 600\n" +
	"checkEndpoint: ${sd-check-endpoint}\n" +
	"metadata:\n" +
	"  model_type: sd\n" +
	"  port: ${PORT}"

// ApplyBodyTemplate substitutes placeholders in a full model-entry body template
// (at 0-indent) and returns the body ready for insertion. Blank lines produced
// by empty optional substitutions are stripped. ttl: -1 is expanded to the
// size-based heuristic (600 s for <10 B params, 0 otherwise).
func ApplyBodyTemplate(body, modelPath, name, mmprojPath, vaePath string) string {
	mmprojLine := ""
	if mmprojPath != "" {
		mmprojLine = "--mmproj " + mmprojPath
	}
	vaeLine := ""
	if vaePath != "" {
		vaeLine = "--vae " + vaePath
	}
	body = strings.ReplaceAll(body, PlaceholderModelPath, modelPath)
	body = strings.ReplaceAll(body, PlaceholderModelName, name)
	body = strings.ReplaceAll(body, PlaceholderMmprojLine, mmprojLine)
	body = strings.ReplaceAll(body, PlaceholderVaeLine, vaeLine)
	// ttl: -1 is a magic value: expand to the size-based heuristic.
	autoTTL := strconv.Itoa(llmTTL(name))
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		t := strings.TrimLeft(l, " \t")
		if t == "ttl: -1" {
			lines[i] = l[:len(l)-len(t)] + "ttl: " + autoTTL
		}
	}
	return stripBlankLines(strings.Join(lines, "\n"))
}

// stripBlankLines removes lines that are empty or whitespace-only.
func stripBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	out := lines[:0]
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}
