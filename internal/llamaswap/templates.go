package llamaswap

import "strings"

// Templates holds the cmd format templates and default TTL values used when
// generating new model entries in config.yaml.
type Templates struct {
	LLMCmd string `json:"llmCmd"`
	// LLMTtl is the TTL in seconds for new LLM entries.
	// -1 means "auto" (600 s for models under 10 B params, 0 otherwise).
	LLMTtl          int    `json:"llmTtl"`
	SDCmd            string `json:"sdCmd"`
	SDTtl            int    `json:"sdTtl"`
	SDCheckEndpoint  string `json:"sdCheckEndpoint"`
}

// Placeholder tokens used in cmd templates.
const (
	PlaceholderModelPath  = "{{MODEL_PATH}}"
	PlaceholderModelName  = "{{MODEL_NAME}}"
	PlaceholderMmprojLine = "{{MMPROJ_LINE}}"
	PlaceholderVaeLine    = "{{VAE_LINE}}"
)

// DefaultLLMCmd is the built-in LLM command template.
// Uses llama-swap macro expansion; sub-args are indented 2 spaces so the
// generated YAML literal block matches the recommended config.yaml style.
const DefaultLLMCmd = "${llama-command-template}\n" +
	"  -c ${default-context}\n" +
	"  -m {{MODEL_PATH}}\n" +
	"  {{MMPROJ_LINE}}\n" +
	"  --alias {{MODEL_NAME}}"

// DefaultSDCmd is the built-in SD command template.
const DefaultSDCmd = "${sd-command-template}\n" +
	"  --diffusion-model {{MODEL_PATH}}\n" +
	"  {{VAE_LINE}}"

// DefaultSDCheckEndpoint is the default checkEndpoint for SD model entries.
const DefaultSDCheckEndpoint = "${sd-check-endpoint}"

// DefaultTemplates returns the built-in fallback templates.
func DefaultTemplates() Templates {
	return Templates{
		LLMCmd:          DefaultLLMCmd,
		LLMTtl:          -1,
		SDCmd:           DefaultSDCmd,
		SDTtl:           600,
		SDCheckEndpoint: DefaultSDCheckEndpoint,
	}
}

// ApplyLLMCmd substitutes placeholders in the LLM cmd template and returns
// the final command string with blank lines stripped.
func ApplyLLMCmd(tpl Templates, modelPath, name, mmprojPath string) string {
	mmprojLine := ""
	if mmprojPath != "" {
		mmprojLine = "--mmproj " + mmprojPath
	}
	cmd := tpl.LLMCmd
	cmd = strings.ReplaceAll(cmd, PlaceholderModelPath, modelPath)
	cmd = strings.ReplaceAll(cmd, PlaceholderModelName, name)
	cmd = strings.ReplaceAll(cmd, PlaceholderMmprojLine, mmprojLine)
	return stripBlankLines(cmd)
}

// ApplySDCmd substitutes placeholders in the SD cmd template and returns the
// final command string with blank lines stripped.
func ApplySDCmd(tpl Templates, modelPath, vaePath string) string {
	vaeLine := ""
	if vaePath != "" {
		vaeLine = "--vae " + vaePath
	}
	cmd := tpl.SDCmd
	cmd = strings.ReplaceAll(cmd, PlaceholderModelPath, modelPath)
	cmd = strings.ReplaceAll(cmd, PlaceholderVaeLine, vaeLine)
	return stripBlankLines(cmd)
}

// LLMTtlFor returns the effective TTL for an LLM. If tpl.LLMTtl is -1 the
// size-based heuristic is applied; otherwise the template value is used.
func LLMTtlFor(tpl Templates, name string) int {
	if tpl.LLMTtl == -1 {
		return llmTTL(name)
	}
	return tpl.LLMTtl
}

// stripBlankLines removes lines that are empty or whitespace-only, which can
// appear when an optional placeholder like {{MMPROJ_LINE}} is substituted with
// an empty string.
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
