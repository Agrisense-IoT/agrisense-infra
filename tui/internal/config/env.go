package config

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// GeneratableKeys is the set of keys that can be auto-generated.
var GeneratableKeys = map[string]bool{
	"JWT_SECRET":                   true,
	"ANON_KEY":                     true,
	"SERVICE_ROLE_KEY":             true,
	"SECRET_KEY_BASE":              true,
	"VAULT_ENC_KEY":                true,
	"PG_META_CRYPTO_KEY":           true,
	"LOGFLARE_PUBLIC_ACCESS_TOKEN": true,
	"LOGFLARE_PRIVATE_ACCESS_TOKEN": true,
	"S3_PROTOCOL_ACCESS_KEY_ID":    true,
	"S3_PROTOCOL_ACCESS_KEY_SECRET": true,
	"DASHBOARD_PASSWORD":           true,
	"POSTGRES_PASSWORD":            true,
	"SMTP_PASS":                    true,
	"MINIO_ROOT_PASSWORD":          true,
}

var sensitiveRe = regexp.MustCompile(`(?i)(PASSWORD|SECRET|KEY|TOKEN)`)
var placeholderRe = regexp.MustCompile(`^<.*>$`)

// EnvField represents a single key=value pair from the .env file.
type EnvField struct {
	Key          string
	Value        string
	Label        string
	Description  string
	Generatable  bool
	IsSensitive  bool
	IsPlaceholder bool
}

// EnvSection is a named group of items.
type EnvSection struct {
	Name  string
	Items []EnvItem
}

// EnvItem is either a separator header or a field.
type EnvItem struct {
	IsSeparator bool
	Label       string
	Field       *EnvField
}

// ParseEnvWithExample reads .env.example for structure and .env for current values.
// It returns sections preserving the example's layout.
func ParseEnvWithExample(envPath, examplePath string) ([]EnvSection, error) {
	current, err := ReadEnvValues(envPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading .env: %w", err)
	}
	if current == nil {
		current = map[string]string{}
	}

	f, err := os.Open(examplePath)
	if err != nil {
		return nil, fmt.Errorf("opening .env.example: %w", err)
	}
	defer f.Close()

	var sections []EnvSection
	var cur *EnvSection

	scanner := bufio.NewScanner(f)
	var pendingLabel, pendingDesc string

	for scanner.Scan() {
		line := scanner.Text()

		// Section separator: ## Section Name
		if strings.HasPrefix(line, "## ") {
			if cur != nil {
				sections = append(sections, *cur)
			}
			name := strings.TrimPrefix(line, "## ")
			cur = &EnvSection{Name: strings.TrimSpace(name)}
			pendingLabel, pendingDesc = "", ""
			continue
		}

		// Label comment: # Label: something
		if strings.HasPrefix(line, "# Label:") {
			pendingLabel = strings.TrimSpace(strings.TrimPrefix(line, "# Label:"))
			continue
		}

		// Description comment: # Description: something or just # something
		if strings.HasPrefix(line, "# ") {
			pendingDesc = strings.TrimSpace(strings.TrimPrefix(line, "# "))
			continue
		}

		// Blank line or pure comment reset
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			if strings.TrimSpace(line) == "" {
				pendingLabel, pendingDesc = "", ""
			}
			continue
		}

		// Key=Value line
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		exampleVal := strings.TrimSpace(line[idx+1:])

		val, ok := current[key]
		if !ok {
			val = exampleVal
		}

		label := pendingLabel
		if label == "" {
			label = key
		}

		field := &EnvField{
			Key:           key,
			Value:         val,
			Label:         label,
			Description:   pendingDesc,
			Generatable:   GeneratableKeys[key],
			IsSensitive:   sensitiveRe.MatchString(key),
			IsPlaceholder: placeholderRe.MatchString(val),
		}
		pendingLabel, pendingDesc = "", ""

		if cur == nil {
			cur = &EnvSection{Name: "General"}
		}
		cur.Items = append(cur.Items, EnvItem{Field: field})
	}

	if cur != nil {
		sections = append(sections, *cur)
	}

	return sections, scanner.Err()
}

// WriteEnv writes sections back to envPath using .env.example as a template.
// Values in sections override the example values; structure is preserved.
func WriteEnv(envPath, examplePath string, sections []EnvSection) error {
	// Build a flat map of key→value from sections
	vals := map[string]string{}
	for _, sec := range sections {
		for _, item := range sec.Items {
			if !item.IsSeparator && item.Field != nil {
				vals[item.Field.Key] = item.Field.Value
			}
		}
	}

	// Read example template
	f, err := os.Open(examplePath)
	if err != nil {
		return fmt.Errorf("opening .env.example: %w", err)
	}
	defer f.Close()

	var out strings.Builder
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		idx := strings.IndexByte(line, '=')
		if idx >= 0 && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			key := strings.TrimSpace(line[:idx])
			if v, ok := vals[key]; ok {
				out.WriteString(key + "=" + v + "\n")
				continue
			}
		}
		out.WriteString(line + "\n")
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	return os.WriteFile(envPath, []byte(out.String()), 0600)
}

// ReadEnvValues reads a .env file and returns a flat map of key→value.
// Comments and blank lines are ignored.
func ReadEnvValues(envPath string) (map[string]string, error) {
	f, err := os.Open(envPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	m := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "#") || strings.TrimSpace(line) == "" {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		// Strip surrounding quotes if present
		if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
			val = val[1 : len(val)-1]
		}
		m[key] = val
	}
	return m, scanner.Err()
}

// FindMissingFields returns a list of keys whose values are still placeholders (<...>).
func FindMissingFields(sections []EnvSection) []string {
	var missing []string
	for _, sec := range sections {
		for _, item := range sec.Items {
			if !item.IsSeparator && item.Field != nil && item.Field.IsPlaceholder {
				missing = append(missing, item.Field.Key)
			}
		}
	}
	return missing
}
